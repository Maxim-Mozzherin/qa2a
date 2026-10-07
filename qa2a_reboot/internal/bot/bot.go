package bot

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"qa2a/internal/repository"
)

type Bot struct {
	Token      string
	AdminID    int64
	TargetChat string
	Repo       *repository.Repository
	HTTPClient *http.Client
}

type updateResponse struct {
	Ok     bool `json:"ok"`
	Result []struct {
		UpdateID int `json:"update_id"`
		Message  *struct {
			From struct {
				ID int64 `json:"id"`
			} `json:"from"`
			Text string `json:"text"`
			Chat struct {
				ID int64 `json:"id"`
			} `json:"chat"`
		} `json:"message"`
	} `json:"result"`
}

func New(token string, adminID int64, repo *repository.Repository) *Bot {
	target := os.Getenv("TELEGRAM_NOTIFICATION_CHAT")
	if target == "" {
		target = "@qa2a_team"
	}
	return &Bot{
		Token:      token,
		AdminID:    adminID,
		TargetChat: target,
		Repo:       repo,
		HTTPClient: &http.Client{Timeout: 30 * time.Second},
	}
}

func (b *Bot) Start() {
	if b.Token == "" {
		return
	}
	go b.poll()
}

func (b *Bot) poll() {
	offset := 0
	client := &http.Client{Timeout: 60 * time.Second}
	
	for {
		url := fmt.Sprintf("https://api.telegram.org/bot%s/getUpdates?offset=%d&timeout=50", b.Token, offset)
		resp, err := client.Get(url)
		if err != nil {
			time.Sleep(5 * time.Second)
			continue
		}
		
		var res updateResponse
		err = json.NewDecoder(resp.Body).Decode(&res)
		resp.Body.Close()
		
		if err != nil || !res.Ok {
			time.Sleep(5 * time.Second)
			continue
		}
		
		for _, u := range res.Result {
			if u.UpdateID >= offset {
				offset = u.UpdateID + 1
			}
			if u.Message != nil && u.Message.Text != "" && u.Message.From.ID == b.AdminID {
				b.handleCommand(strings.TrimSpace(u.Message.Text))
			}
		}
	}
}

func (b *Bot) handleCommand(text string) {
	if strings.HasPrefix(text, "/stats") {
		b.sendStats()
	} else if strings.HasPrefix(text, "/backup") {
		b.SendFullBackup()
	} else if strings.HasPrefix(text, "/restart") {
		b.SendText("🔄 Инициирован перезапуск сервисов (qa2a, iiko-parser)...")
		go func() {
			time.Sleep(1 * time.Second) // Даем время на отправку сообщения
			exec.Command("systemctl", "restart", "iiko-parser").Run()
			exec.Command("systemctl", "restart", "iiko_parser").Run()
			exec.Command("systemctl", "restart", "qa2a").Run()
		}()
	}
}

func (b *Bot) SendText(text string) {
	target := b.TargetChat
	if target == "" {
		target = "@qa2a_team"
	}
	_ = b.SendToTarget(target, text)
}

func (b *Bot) SendToTarget(target string, text string) error {
	if b.Token == "" || target == "" {
		return fmt.Errorf("bot token is empty or invalid target")
	}
	client := b.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", b.Token)
	payload, _ := json.Marshal(map[string]interface{}{
		"chat_id":    target,
		"text":       text,
		"parse_mode": "HTML",
	})
	resp, err := client.Post(url, "application/json", bytes.NewBuffer(payload))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return fmt.Errorf("telegram api error: %s", string(respBody))
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	return nil
}

func (b *Bot) SendToChat(chatID int64, text string) error {
	if b.TargetChat != "" {
		return b.SendToTarget(b.TargetChat, text)
	}
	return b.SendToTarget(fmt.Sprintf("%d", chatID), text)
}

func getSystemStats() (string, string, string) {
	// 1. RAM via /proc/meminfo
	ram := "N/A"
	if memBytes, err := os.ReadFile("/proc/meminfo"); err == nil {
		var memTotal, memAvail uint64
		for _, line := range strings.Split(string(memBytes), "\n") {
			if strings.HasPrefix(line, "MemTotal:") {
				fmt.Sscanf(line, "MemTotal: %d kB", &memTotal)
			} else if strings.HasPrefix(line, "MemAvailable:") {
				fmt.Sscanf(line, "MemAvailable: %d kB", &memAvail)
			}
		}
		if memTotal > 0 {
			used := memTotal - memAvail
			usedMB := used / 1024
			totalMB := memTotal / 1024
			pct := float64(used) * 100.0 / float64(memTotal)
			ram = fmt.Sprintf("%.1f%% (%d / %d MB)", pct, usedMB, totalMB)
		}
	}

	// 2. Load Average via /proc/loadavg
	cpu := "N/A"
	if loadBytes, err := os.ReadFile("/proc/loadavg"); err == nil {
		fields := strings.Fields(string(loadBytes))
		if len(fields) >= 3 {
			cpu = fmt.Sprintf("%s, %s, %s (1, 5, 15 мин)", fields[0], fields[1], fields[2])
		}
	}

	// 3. Uptime via uptime command or /proc/uptime
	uptime := "N/A"
	if upOut, err := exec.Command("uptime", "-p").Output(); err == nil && len(upOut) > 0 {
		uptime = strings.TrimSpace(string(upOut))
	} else if upBytes, err := os.ReadFile("/proc/uptime"); err == nil {
		var sec float64
		if _, err := fmt.Sscanf(string(upBytes), "%f", &sec); err == nil {
			hours := int(sec) / 3600
			mins := (int(sec) % 3600) / 60
			uptime = fmt.Sprintf("up %d часов, %d минут", hours, mins)
		}
	}

	return cpu, ram, uptime
}

func (b *Bot) sendStats() {
	cpu, ram, uptime := getSystemStats()

	var users, totalComp, connectedComp, operations int
	if b.Repo != nil && b.Repo.GetDb() != nil {
		b.Repo.GetDb().Get(&users, "SELECT count(*) FROM users")
		b.Repo.GetDb().Get(&totalComp, "SELECT count(*) FROM companies")
		b.Repo.GetDb().Get(&connectedComp, "SELECT count(*) FROM companies WHERE iiko_api_login IS NOT NULL AND iiko_api_login != ''")
		b.Repo.GetDb().Get(&operations, "SELECT count(*) FROM operations")
	}

	text := fmt.Sprintf("📊 <b>Панель управления QA2A</b>\n\n"+
		"⏱ <b>Аптайм:</b> %s\n\n"+
		"🖥 <b>Система:</b>\n"+
		"├ CPU (Load): %s\n"+
		"└ RAM: %s\n\n"+
		"👥 <b>Аудитория & Клиенты:</b>\n"+
		"├ Всего юзеров: %d\n"+
		"├ Заведений всего: %d\n"+
		"└ С активной iiko: %d\n\n"+
		"📝 <b>Складские документы:</b>\n"+
		"└ Всего проводок: %d\n\n"+
		"Доступные команды:\n"+
		"/stats - статистика\n"+
		"/backup - ручной бэкап\n"+
		"/restart - рестарт сервисов",
		uptime, cpu, ram, users, totalComp, connectedComp, operations)

	b.SendText(text)
}

func (b *Bot) SendFullBackup() {
	b.SendText("📦 Запускаю процесс создания полного бэкапа...")

	// 1. Dump Database directly via exec without shell redirection
	sqlPath := "/opt/qa2a-reboot/database.sql"
	sqlFile, err := os.Create(sqlPath)
	if err != nil {
		b.SendText("❌ Ошибка создания файла дампа: " + err.Error())
		log.Printf("[Bot Backup] Create dump file failed: %v", err)
		return
	}

	// Пробуем дамп через возможные имена Docker-контейнеров или нативный pg_dump
	dumpSuccess := false
	var lastDumpErr string

	containerCandidates := []string{"qa2a-dev-db", "qa2a-postgres", "qa2a_dev_db", "postgres"}
	for _, cName := range containerCandidates {
		cmd := exec.Command("docker", "exec", cName, "pg_dump", "-U", "admin", "-d", "qa2a")
		cmd.Stdout = sqlFile
		var errBuf bytes.Buffer
		cmd.Stderr = &errBuf
		if err := cmd.Run(); err == nil {
			dumpSuccess = true
			break
		} else {
			lastDumpErr = errBuf.String()
		}
	}

	if !dumpSuccess {
		// Fallback: нативный pg_dump на локальном порту СУБД (5433 / 5432 / 5434)
		nativePorts := []string{"5433", "5432", "5434"}
		for _, port := range nativePorts {
			cmd := exec.Command("pg_dump", "-h", "localhost", "-p", port, "-U", "admin", "-d", "qa2a")
			cmd.Stdout = sqlFile
			var errBuf bytes.Buffer
			cmd.Stderr = &errBuf
			if err := cmd.Run(); err == nil {
				dumpSuccess = true
				break
			} else {
				lastDumpErr = errBuf.String()
			}
		}
	}

	sqlFile.Close()
	if !dumpSuccess {
		os.Remove(sqlPath)
		b.SendText("❌ Ошибка дампа БД: " + lastDumpErr)
		log.Printf("[Bot Backup] DB dump failed, last stderr: %s", lastDumpErr)
		return
	}

	// 2. Archive everything directly via tar without shell
	archivePath := "/tmp/qa2a_full_backup.tar.gz"
	tarCmd := exec.Command("tar", "-czf", archivePath,
		"--exclude=.git",
		"--exclude=*.env",
		"--exclude=.env*",
		"--exclude=.superadmin_credentials",
		"--exclude=*.exe",
		"--exclude=*backup*",
		"--exclude=*_backup*",
		"--exclude=qa2a-reboot/qa2a",
		"--exclude=qa2a-reboot/qa2a_app",
		"--exclude=iiko_parser/iiko",
		"--exclude=iiko_parser/iiko-parser",
		"--exclude=iiko_parser/iiko_parser",
		"--exclude=iiko_parser/parser_app",
		"--exclude=iiko_parser/temp/*",
		"--exclude=*.tar.gz",
		"-C", "/opt",
		"qa2a-reboot", "iiko_parser",
	)
	var tarErr bytes.Buffer
	tarCmd.Stderr = &tarErr

	if err := tarCmd.Run(); err != nil {
		os.Remove(sqlPath)
		b.SendText("❌ Ошибка архивации: " + err.Error() + "\n" + tarErr.String())
		log.Printf("[Bot Backup] Tar failed: %v, stderr: %s", err, tarErr.String())
		return
	}

	defer os.Remove(archivePath)
	defer os.Remove(sqlPath)

	fi, err := os.Stat(archivePath)
	if err != nil {
		b.SendText("❌ Не удалось прочитать архив: " + err.Error())
		return
	}

	sizeMB := float64(fi.Size()) / (1024 * 1024)

	// Лимит Telegram Bot API на отправку одного документа — 50 МБ
	const tgMaxFileSize = 48 * 1024 * 1024
	if fi.Size() > tgMaxFileSize {
		b.SendText(fmt.Sprintf("⚠️ Размер архива (%.1f МБ) превышает лимит Telegram (50 МБ). Разбиваю на части...", sizeMB))

		partPrefix := "/tmp/qa2a_full_backup.tar.gz.part_"
		splitCmd := exec.Command("split", "-b", "45M", archivePath, partPrefix)
		if err := splitCmd.Run(); err != nil {
			b.SendText("❌ Не удалось разбить архив: " + err.Error())
			return
		}

		parts, _ := filepath.Glob(partPrefix + "*")
		defer func() {
			for _, p := range parts {
				os.Remove(p)
			}
		}()

		totalParts := len(parts)
		for idx, part := range parts {
			caption := fmt.Sprintf("📦 <b>Часть %d из %d</b> полного бэкапа\n\nДля объединения на сервере:\n<code>cat qa2a_full_backup.tar.gz.part_* > qa2a_full_backup.tar.gz</code>", idx+1, totalParts)
			if err := b.sendDocument(part, caption); err != nil {
				b.SendText(fmt.Sprintf("❌ Ошибка отправки части %d: %s", idx+1, err.Error()))
				return
			}
			time.Sleep(1 * time.Second)
		}
		b.SendText("✅ Все части бэкапа успешно отправлены!")
		return
	}

	caption := fmt.Sprintf("📦 <b>Полный бэкап сервера</b> (%.1f МБ) #backup\n\nВнутри:\n📁 Файлы проекта (<code>/opt/qa2a-reboot</code>, <code>/opt/iiko_parser</code>)\n🗄 Дамп базы данных (<code>database.sql</code>)", sizeMB)
	if err := b.sendDocument(archivePath, caption); err != nil {
		b.SendText("❌ Ошибка API Telegram при отправке файла: " + err.Error())
		return
	}

	b.SendText("✅ Бэкап успешно отправлен!")
}

func (b *Bot) sendDocument(filePath, caption string) error {
	pr, pw := io.Pipe()
	writer := multipart.NewWriter(pw)

	go func() {
		var err error
		defer func() {
			if err != nil {
				pw.CloseWithError(err)
			} else {
				pw.Close()
			}
		}()

		targetChat := b.TargetChat
		if targetChat == "" {
			targetChat = "@qa2a_team"
		}
		if err = writer.WriteField("chat_id", targetChat); err != nil {
			return
		}
		if caption != "" {
			if err = writer.WriteField("caption", caption); err != nil {
				return
			}
			if err = writer.WriteField("parse_mode", "HTML"); err != nil {
				return
			}
		}

		part, err := writer.CreateFormFile("document", filepath.Base(filePath))
		if err != nil {
			return
		}

		file, err := os.Open(filePath)
		if err != nil {
			return
		}
		defer file.Close()

		_, err = io.Copy(part, file)
		if err != nil {
			return
		}
		err = writer.Close()
	}()

	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendDocument", b.Token)
	req, err := http.NewRequest("POST", url, pr)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return fmt.Errorf("%s", string(respBody))
	}
	return nil
}

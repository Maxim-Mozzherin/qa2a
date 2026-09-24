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
	Token   string
	AdminID int64
	Repo    *repository.Repository
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
	return &Bot{
		Token:   token,
		AdminID: adminID,
		Repo:    repo,
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
		b.SendText("🔄 Инициирован перезапуск сервисов (qa2a, iiko_parser)...")
		go func() {
			time.Sleep(1 * time.Second) // Даем время на отправку сообщения
			exec.Command("systemctl", "restart", "iiko_parser").Run()
			exec.Command("systemctl", "restart", "qa2a").Run()
		}()
	}
}

func (b *Bot) SendText(text string) {
	if b.Token == "" {
		return
	}
	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", b.Token)
	payload, _ := json.Marshal(map[string]interface{}{
		"chat_id":    b.AdminID,
		"text":       text,
		"parse_mode": "HTML",
	})
	http.Post(url, "application/json", bytes.NewBuffer(payload))
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

	dumpCmd := exec.Command("docker", "exec", "qa2a-postgres", "pg_dump", "-U", "admin", "-d", "qa2a")
	dumpCmd.Stdout = sqlFile
	var dumpErr bytes.Buffer
	dumpCmd.Stderr = &dumpErr

	if err := dumpCmd.Run(); err != nil {
		sqlFile.Close()
		os.Remove(sqlPath)
		b.SendText("❌ Ошибка дампа БД: " + err.Error() + "\n" + dumpErr.String())
		log.Printf("[Bot Backup] DB dump failed: %v, stderr: %s", err, dumpErr.String())
		return
	}
	sqlFile.Close()

	// 2. Archive everything directly via tar without shell
	archivePath := "/tmp/qa2a_full_backup.tar.gz"
	tarCmd := exec.Command("tar", "-czf", archivePath,
		"--exclude=.git",
		"--exclude=qa2a",
		"--exclude=qa2a_app",
		"--exclude=iiko_parser/iiko_parser",
		"--exclude=iiko_parser/parser_app",
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

	// 3. Send file to Telegram
	file, err := os.Open(archivePath)
	if err != nil {
		b.SendText("❌ Не удалось открыть архив: " + err.Error())
		return
	}
	defer file.Close()

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	
	_ = writer.WriteField("chat_id", fmt.Sprintf("%d", b.AdminID))
	_ = writer.WriteField("caption", "📦 <b>Полный бэкап сервера</b> #backup\n\nВнутри:\n📁 Файлы проекта (<code>/opt/qa2a-reboot</code>, <code>/opt/iiko_parser</code>)\n🗄 Дамп базы данных (<code>database.sql</code>)")
	_ = writer.WriteField("parse_mode", "HTML")

	part, _ := writer.CreateFormFile("document", filepath.Base(archivePath))
	io.Copy(part, file)
	writer.Close()

	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendDocument", b.Token)
	req, _ := http.NewRequest("POST", url, body)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		b.SendText("❌ Ошибка отправки архива: " + err.Error())
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		b.SendText("❌ Ошибка API Telegram при отправке файла: " + string(respBody))
	} else {
		b.SendText("✅ Бэкап успешно отправлен!")
	}
}

package scheduler

import (
	"database/sql"
	"fmt"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"analytics_service/internal/llm"
	"analytics_service/internal/queue"
	"analytics_service/internal/report"
	"analytics_service/internal/telegram"
)

// AnalyticsScheduler управляет ежедневным регламентным автообновлением накладных
// и отправкой регулярных еженедельных и ежемесячных отчетов по подписке в Telegram
type AnalyticsScheduler struct {
	db        *sql.DB
	queue     *queue.SyncQueue
	tgBot     *telegram.BotClient
	llmClient *llm.Client
	fontPath  string
	isRunning atomic.Bool
	quitChan  chan struct{}
	wg        sync.WaitGroup
}

// NewScheduler создает новый экземпляр планировщика
func NewScheduler(db *sql.DB, q *queue.SyncQueue, tgBot *telegram.BotClient, llmClient *llm.Client, fontPath string) *AnalyticsScheduler {
	return &AnalyticsScheduler{
		db:        db,
		queue:     q,
		tgBot:     tgBot,
		llmClient: llmClient,
		fontPath:  fontPath,
		quitChan:  make(chan struct{}),
	}
}

// Start запускает рабочий цикл планировщика
func (s *AnalyticsScheduler) Start() {
	if !s.isRunning.CompareAndSwap(false, true) {
		log.Println("⚠️ [Scheduler] Планировщик уже запущен")
		return
	}
	s.quitChan = make(chan struct{})
	s.wg.Add(1)
	go s.loop()
	log.Println("🚀 [Scheduler] Фоновый планировщик автообновлений и рассылки отчетов запущен")
}

// Stop останавливает планировщик
func (s *AnalyticsScheduler) Stop() {
	if s.isRunning.CompareAndSwap(true, false) {
		close(s.quitChan)
		s.wg.Wait()
		log.Println("🛑 [Scheduler] Фоновый планировщик остановлен")
	}
}

func (s *AnalyticsScheduler) loop() {
	defer s.wg.Done()

	// Первый запуск проверки через 1 минуту после старта сервиса
	initialTimer := time.NewTimer(1 * time.Minute)
	select {
	case <-s.quitChan:
		initialTimer.Stop()
		return
	case <-initialTimer.C:
		s.runDailyMaintenance()
		s.runSubscriptionReports()
	}

	// Далее регулярная проверка каждый час
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-s.quitChan:
			return
		case <-ticker.C:
			s.runDailyMaintenance()
			s.runSubscriptionReports()
		}
	}
}

// runDailyMaintenance проверяет активные рестораны и ставит в очередь автообновление накладных
func (s *AnalyticsScheduler) runDailyMaintenance() {
	rows, err := s.db.Query(`
		SELECT id, name, COALESCE(last_synced_at, '1970-01-01'::timestamptz)
		FROM analytics_restaurants
		WHERE is_active = true AND (company_id != 10 OR company_id IS NULL)
	`)
	if err != nil {
		log.Printf("⚠️ [Scheduler] Ошибка запроса заведений для автообновления: %v", err)
		return
	}
	defer rows.Close()

	now := time.Now()
	type restItem struct {
		id         int
		name       string
		lastSynced time.Time
	}
	var targets []restItem

	for rows.Next() {
		var it restItem
		if errScan := rows.Scan(&it.id, &it.name, &it.lastSynced); errScan == nil {
			targets = append(targets, it)
		}
	}

	for _, it := range targets {
		// Если заведение не синхронизировалось за последние 20 часов — ставим в очередь
		if now.Sub(it.lastSynced) >= 20*time.Hour {
			var invCount int
			_ = s.db.QueryRow(`SELECT COUNT(*) FROM analytics_invoices WHERE restaurant_id = $1`, it.id).Scan(&invCount)

			from := now.AddDate(0, 0, -7).Format("2006-01-02") // инкрементальная выгрузка за 7 дней
			if invCount == 0 {
				from = now.AddDate(0, 0, -60).Format("2006-01-02") // первая полная выгрузка за 60 дней
			}
			to := now.Format("2006-01-02")

			s.queue.Enqueue(queue.SyncTask{
				RestaurantID:   it.id,
				RestaurantName: it.name,
				From:           from,
				To:             to,
				Trigger:        "daily_auto",
			})
		}
	}
}

// runSubscriptionReports проверяет наступление 7-дневных и 30-дневных сроков отчетов
func (s *AnalyticsScheduler) runSubscriptionReports() {
	rows, err := s.db.Query(`
		SELECT 
			id, name, subscription_started_at, telegram_recipients, 
			last_weekly_report_at, last_monthly_report_at
		FROM analytics_restaurants
		WHERE is_active = true 
		  AND is_subscribed = true 
		  AND subscription_started_at IS NOT NULL
		  AND length(telegram_recipients) > 0
		  AND (company_id != 10 OR company_id IS NULL)
	`)
	if err != nil {
		log.Printf("⚠️ [Scheduler] Ошибка выборки заведений с подпиской: %v", err)
		return
	}
	defer rows.Close()

	now := time.Now()

	for rows.Next() {
		var id int
		var name, recipients string
		var subStarted time.Time
		var lastWeekly, lastMonthly sql.NullTime

		if errScan := rows.Scan(&id, &name, &subStarted, &recipients, &lastWeekly, &lastMonthly); errScan != nil {
			continue
		}

		chatIDs, unresolved := s.tgBot.ResolveRecipients(recipients)
		if len(unresolved) > 0 {
			log.Printf("ℹ️ [Scheduler] Для заведения %s (%d) не найдены chat_id для получателей: %v (требуется /start в боте)", name, id, unresolved)
		}
		if len(chatIDs) == 0 {
			continue
		}

		daysSinceSub := int(now.Sub(subStarted).Hours() / 24)

		// 1. ПРОВЕРКА ЕЖЕНЕДЕЛЬНОГО ОТЧЕТА (каждые 7 дней)
		// Если прошло >= 7 дней с момента старта и с момента последнего отчета прошло >= 6 дней
		needsWeekly := false
		if daysSinceSub >= 7 {
			if !lastWeekly.Valid || now.Sub(lastWeekly.Time).Hours() >= (6*24+12) {
				needsWeekly = true
			}
		}

		if needsWeekly {
			weekNum := (daysSinceSub / 7)
			if weekNum < 1 {
				weekNum = 1
			}
			from := now.AddDate(0, 0, -7).Format("2006-01-02")
			to := now.Format("2006-01-02")

			log.Printf("📬 [Scheduler] Отправка еженедельного отчета (Неделя #%d) для %s (%d)...", weekNum, name, id)
			repData, errRep := report.BuildWeeklyReport(s.db, id, from, to, weekNum)
			if errRep == nil {
				msgHTML := report.FormatTelegramWeeklyHTML(repData)
				sentCount := 0
				for _, cid := range chatIDs {
					if errSend := s.tgBot.SendMessage(cid, msgHTML); errSend == nil {
						sentCount++
					} else {
						log.Printf("⚠️ [Scheduler] Ошибка отправки еженедельного отчета в чат %d: %v", cid, errSend)
					}
				}
				if sentCount > 0 {
					_, _ = s.db.Exec(`UPDATE analytics_restaurants SET last_weekly_report_at = NOW() WHERE id = $1`, id)
					log.Printf("✅ [Scheduler] Еженедельный отчет для %s доставлен %d получателям", name, sentCount)
				}
			} else {
				log.Printf("❌ [Scheduler] Ошибка формирования еженедельного отчета для %s: %v", name, errRep)
			}
		}

		// 2. ПРОВЕРКА МЕСЯЧНОГО PDF-ОТЧЕТА (через 30 дней)
		needsMonthly := false
		if daysSinceSub >= 30 {
			if !lastMonthly.Valid || now.Sub(lastMonthly.Time).Hours() >= (29 * 24) {
				needsMonthly = true
			}
		}

		if needsMonthly {
			from := now.AddDate(0, 0, -30).Format("2006-01-02")
			to := now.Format("2006-01-02")

			log.Printf("📑 [Scheduler] Генерация и отправка 30-дневного PDF-отчета для %s (%d)...", name, id)
			pdfBytes, errPDF := report.GenerateMonthlyPDF(s.db, s.llmClient, id, from, to, s.fontPath)
			if errPDF == nil {
				filename := fmt.Sprintf("Аудит_закупок_%s_30_дней.pdf", cleanFilename(name))
				caption := fmt.Sprintf(
					"<b>Управленческий аудит закупок за 30 дней</b>\n\n"+
						"Заведение: <b>%s</b>\n"+
						"Период анализа: <b>%s — %s</b>\n\n"+
						"В приложении сформирован ежемесячный PDF-отчет с анализом ключевых показателей, графиками динамики цен и детализацией по поставщикам.\n\n"+
						"Обратите внимание: отчет сформирован за 30 дней от текущей даты. Все показатели рассчитаны как средневзвешенные значения.\n\n"+
						"Сервис ресторанной аналитики QA2A.",
					name, from, to,
				)

				sentCount := 0
				for _, cid := range chatIDs {
					if errSend := s.tgBot.SendDocument(cid, filename, pdfBytes, caption); errSend == nil {
						sentCount++
					} else {
						log.Printf("⚠️ [Scheduler] Ошибка отправки PDF в чат %d: %v", cid, errSend)
					}
				}
				if sentCount > 0 {
					_, _ = s.db.Exec(`UPDATE analytics_restaurants SET last_monthly_report_at = NOW() WHERE id = $1`, id)
					log.Printf("✅ [Scheduler] Месячный PDF-отчет для %s успешно отправлен %d получателям", name, sentCount)
				}
			} else {
				log.Printf("❌ [Scheduler] Ошибка генерации месячного PDF для %s: %v", name, errPDF)
			}
		}
	}
}

// SendImmediateTestReport позволяет администратору мгновенно отправить тестовый отчет в Telegram
func (s *AnalyticsScheduler) SendImmediateTestReport(restID int, reportType string) (int, error) {
	var name, recipients string
	err := s.db.QueryRow(`
		SELECT name, COALESCE(telegram_recipients, '') 
		FROM analytics_restaurants 
		WHERE id = $1`, restID).Scan(&name, &recipients)
	if err != nil {
		return 0, fmt.Errorf("заведение не найдено: %w", err)
	}

	chatIDs, unresolved := s.tgBot.ResolveRecipients(recipients)
	if len(chatIDs) == 0 {
		return 0, fmt.Errorf("не найдено ни одного активного chat_id в Telegram для получателей: %s. Убедитесь, что указанные пользователи запустили бота /start", recipients)
	}

	now := time.Now()
	sentCount := 0

	if reportType == "pdf" || reportType == "monthly" {
		from := now.AddDate(0, 0, -30).Format("2006-01-02")
		to := now.Format("2006-01-02")
		pdfBytes, err := report.GenerateMonthlyPDF(s.db, s.llmClient, restID, from, to, s.fontPath)
		if err != nil {
			return 0, fmt.Errorf("ошибка сборки PDF: %w", err)
		}

		filename := fmt.Sprintf("Тестовый_аудит_%s_30_дней.pdf", cleanFilename(name))
		caption := fmt.Sprintf(
			"<b>Тестовое уведомление: Ежемесячный аудит закупок за 30 дней</b>\n\n"+
				"Заведение: <b>%s</b>\n"+
				"Период анализа: <b>%s — %s</b>\n\n"+
				"В приложении сформирован образец ежемесячного отчета в формате PDF. Все показатели рассчитаны как средневзвешенные значения за 30 дней.\n\n"+
				"Сервис ресторанной аналитики QA2A.",
			name, from, to,
		)

		for _, cid := range chatIDs {
			if errSend := s.tgBot.SendDocument(cid, filename, pdfBytes, caption); errSend == nil {
				sentCount++
			}
		}
	} else {
		// weekly
		from := now.AddDate(0, 0, -7).Format("2006-01-02")
		to := now.Format("2006-01-02")
		repData, err := report.BuildWeeklyReport(s.db, restID, from, to, 1)
		if err != nil {
			return 0, fmt.Errorf("ошибка расчета недельного отчета: %w", err)
		}

		msgHTML := "<b>Тестовая отправка еженедельного отчета</b>\n\n" + report.FormatTelegramWeeklyHTML(repData)
		for _, cid := range chatIDs {
			if errSend := s.tgBot.SendMessage(cid, msgHTML); errSend == nil {
				sentCount++
			}
		}
	}

	if len(unresolved) > 0 {
		log.Printf("ℹ️ [TestReport] Отправлено %d получателям. Не удалось сопоставить: %v", sentCount, unresolved)
	}
	return sentCount, nil
}

func cleanFilename(s string) string {
	var res []rune
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || (r >= 'а' && r <= 'я') || (r >= 'А' && r <= 'Я') || r == '_' || r == '-' {
			res = append(res, r)
		} else {
			res = append(res, '_')
		}
	}
	return string(res)
}

package service

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"qa2a/internal/repository"
)

// BotAlerter интерфейс отправки оперативных уведомлений (в Telegram @qa2a_team)
type BotAlerter interface {
	SendText(text string)
}

// Scheduler управляет регулярным выполнением фоновых регламентных процедур:
// ежедневная ночная выгрузка списаний и перемещений в iiko RMS, ротация архива заявок.
type Scheduler struct {
	repo             *repository.Repository
	iikoSvc          *IikoService
	syncQueue        *SyncQueue
	alerter          BotAlerter
	isRunning        atomic.Bool
	isExporting      sync.Mutex
	quitChan         chan struct{}
	wg               sync.WaitGroup
	OnExportComplete func() // Callback for automated backup
}

// NewScheduler создает новый экземпляр планировщика фоновых задач.
func NewScheduler(repo *repository.Repository, iikoSvc *IikoService, syncQueue *SyncQueue) *Scheduler {
	return &Scheduler{
		repo:      repo,
		iikoSvc:   iikoSvc,
		syncQueue: syncQueue,
		quitChan:  make(chan struct{}),
	}
}

// SetAlerter устанавливает компонент отправки уведомлений в Telegram
func (s *Scheduler) SetAlerter(a BotAlerter) {
	s.alerter = a
}

// SetSyncQueue устанавливает менеджер очереди синхронизации
func (s *Scheduler) SetSyncQueue(sq *SyncQueue) {
	s.syncQueue = sq
}

// Start запускает цикл фонового планировщика в отдельной горутине.
func (s *Scheduler) Start() {
	if !s.isRunning.CompareAndSwap(false, true) {
		log.Println("[scheduler] ⚠️ Планировщик уже запущен")
		return
	}
	// Fix: Re-initialize the channel safely before starting the loop
	s.quitChan = make(chan struct{})
	s.wg.Add(1)
	go s.scheduleLoop()
}

// Stop безопасно останавливает фоновый планировщик при выключении сервера.
func (s *Scheduler) Stop() {
	if s.isRunning.CompareAndSwap(true, false) {
		close(s.quitChan)
		s.wg.Wait()
		log.Println("[scheduler] 🛑 Фоновый планировщик корректно остановлен")
	}
}

// scheduleLoop организует цикл ожидания до следующего запуска в 06:30 (YEKT / UTC+5).
func (s *Scheduler) scheduleLoop() {
	defer s.wg.Done()

	// Загружаем таймзону Екатеринбурга (Asia/Yekaterinburg / UTC+5)
	loc, err := time.LoadLocation("Asia/Yekaterinburg")
	if err != nil {
		log.Printf("[scheduler] ⚠️ Ошибка загрузки tz 'Asia/Yekaterinburg': %v. Используем фиксированное смещение +05:00", err)
		loc = time.FixedZone("YEKT", 5*60*60)
	}

	for {
		now := time.Now().In(loc)

		// Расчет следующего окна запуска: сегодня 06:30:00 YEKT
		nextRun := time.Date(now.Year(), now.Month(), now.Day(), 6, 30, 0, 0, loc)
		if now.After(nextRun) {
			nextRun = nextRun.Add(24 * time.Hour)
		}

		duration := time.Until(nextRun)
		log.Printf("[scheduler] ⏳ Следующая регламентная выгрузка в iiko запланирована на %s (через %v)",
			nextRun.Format("2006-01-02 15:04:05 YEKT"), duration.Round(time.Minute))

		timer := time.NewTimer(duration)

		select {
		case <-s.quitChan:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return

		case <-timer.C:
			s.executeSafeDailyExport()
			if s.OnExportComplete != nil {
				go func() {
					defer func() {
						if r := recover(); r != nil {
							log.Printf("[scheduler] ❌ Panic in OnExportComplete callback: %v", r)
						}
					}()
					s.OnExportComplete()
				}()
			}
		}
	}
}

// executeSafeDailyExport выполняет регламентные задачи с защитой от паники.
func (s *Scheduler) executeSafeDailyExport() {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[scheduler] ❌ КРИТИЧЕСКАЯ ОШИБКА в регламентной выгрузке (panic recovered): %v", r)
		}
	}()

	s.RunDailyExport()
}

// RunDailyExport выполняет очистку старых заявок и последовательную выгрузку проводок по ресторанам.
func (s *Scheduler) RunDailyExport() {
	// Предотвращаем одновременное наложение нескольких выгрузок
	if !s.isExporting.TryLock() {
		log.Println("[scheduler] ⚠️ Выгрузка уже выполняется в другом потоке. Пропуск итерации.")
		return
	}
	defer s.isExporting.Unlock()

	startTime := time.Now()
	log.Println("[scheduler] 🚀 ЗАПУСК РЕГЛАМЕНТНОЙ ВЫГРУЗКИ В IIKO RMS...")

	// 1. Очистка архива заявок на закупку старше 30 дней
	if err := s.repo.CleanOldProcurements(); err != nil {
		log.Printf("[scheduler] ⚠️ Ошибка очистки архива заявок: %v", err)
	} else {
		log.Println("[scheduler] ✅ Архив заявок очищен от записей старше 30 дней")
	}

	// Очистка медиафайлов заявок старше 14 дней
	func() {
		var oldTickets []struct {
			ID         int    `db:"id"`
			MediaPaths string `db:"media_paths"`
		}
		query := `SELECT id, media_paths FROM accounting_tickets WHERE media_paths != '[]' AND created_at < NOW() - INTERVAL '14 days'`
		if err := s.repo.GetDb().Select(&oldTickets, query); err == nil && len(oldTickets) > 0 {
			deletedCount := 0
			failedCount := 0
			uploadsDir := os.Getenv("UPLOADS_DIR")
			if uploadsDir == "" {
				if _, err := os.Stat("/app/uploads"); err == nil {
					uploadsDir = "/app/uploads"
				} else if _, err := os.Stat("/opt/qa2a-reboot/uploads"); err == nil {
					uploadsDir = "/opt/qa2a-reboot/uploads"
				} else {
					uploadsDir = "uploads"
				}
			}
			ticketsBase := filepath.Join(uploadsDir, "tickets")

			for _, t := range oldTickets {
				var paths []string
				if err := json.Unmarshal([]byte(t.MediaPaths), &paths); err == nil {
					for _, p := range paths {
						cleanName := filepath.Base(p)
						if cleanName != "" && cleanName != "." && cleanName != ".." {
							if err := os.Remove(filepath.Join(ticketsBase, cleanName)); err == nil {
								deletedCount++
							} else {
								failedCount++
							}
						}
					}
				}
				_, _ = s.repo.GetDb().Exec("UPDATE accounting_tickets SET media_paths = '[]' WHERE id = $1", t.ID)
			}
			log.Printf("[scheduler] ✅ Удалено медиа из старых заявок (%d заявок): удалено файлов: %d, ошибок удаления: %d", len(oldTickets), deletedCount, failedCount)
		}
	}()

	// 2. Получение списка активных компаний с настроенным iiko API
	companyIDs, err := s.repo.GetAllActiveCompanyIDs()
	if err != nil {
		log.Printf("[scheduler] ❌ Ошибка выборки активных заведений: %v", err)
		return
	}

	total := len(companyIDs)
	if total == 0 {
		log.Println("[scheduler] Активных интеграций с iiko не найдено.")
		return
	}

	log.Printf("[scheduler] Найдено заведений для синхронизации: %d", total)

	successCount := 0
	failedCount := 0

	// Обрабатываем заведения строго последовательно с паузой для снятия нагрузки с iiko RMS
	for _, cid := range companyIDs {
		// Проверяем, не поступил ли сигнал на остановку сервера
		select {
		case <-s.quitChan:
			log.Println("[scheduler] ⚠️ Выгрузка прервана из-за остановки приложения")
			return
		default:
		}

		if err := s.iikoSvc.ExportDailyOperations(cid, true); err != nil {
			log.Printf("[scheduler] ⚠️ Сбой выгрузки для заведения #%d: %v", cid, err)
			failedCount++
			if s.alerter != nil {
				go s.alerter.SendText(fmt.Sprintf("⚠️ <b>Сбой регламентной выгрузки в iiko (06:30 YEKT)</b>\nЗаведение ID: <code>#%d</code>\nОшибка: <code>%s</code>", cid, err.Error()))
			}
		} else {
			successCount++
		}

		// Задержка 2 секунды между заведениями для предотвращения перегрузки серверов iiko
		time.Sleep(2 * time.Second)
	}

	elapsed := time.Since(startTime).Round(time.Second)
	log.Printf("[scheduler] 🏁 Регламентная выгрузка завершена за %v. Успешно: %d, ошибок: %d (всего: %d)",
		elapsed, successCount, failedCount, total)

	if s.alerter != nil {
		statusEmoji := "✅"
		if failedCount > 0 {
			statusEmoji = "⚠️"
		}
		go s.alerter.SendText(fmt.Sprintf("%s <b>Регламентная выгрузка в iiko (06:30 YEKT) завершена</b>\n⏱ Время: %v\n✅ Успешно заведений: %d\n❌ Ошибок: %d\n📊 Всего: %d",
			statusEmoji, elapsed, successCount, failedCount, total))
	}

	// 3. СТРОГО ПОСЛЕ завершения ночной выгрузки списаний — запускаем синхронизацию номенклатуры через безопасную очередь
	if s.syncQueue != nil {
		log.Println("[scheduler] 🔄 Выгрузка списаний завершена. Запуск регламентной синхронизации номенклатуры по заведениям...")
		s.RunDailyNomenclatureSync()
	}
}

// RunDailyNomenclatureSync ставит все активные заведения в очередь синхронизации номенклатуры.
// Выполняется строго ПОСЛЕ завершения ночной выгрузки списаний.
func (s *Scheduler) RunDailyNomenclatureSync() {
	if s.syncQueue == nil {
		return
	}
	companyIDs, err := s.repo.GetAllActiveCompanyIDs()
	if err != nil {
		log.Printf("[scheduler] ❌ Ошибка выборки активных заведений для синхронизации номенклатуры: %v", err)
		return
	}
	log.Printf("[scheduler] 📋 Постановка %d активных заведений в очередь синхронизации номенклатуры...", len(companyIDs))
	queued := 0
	for _, cid := range companyIDs {
		if s.syncQueue.Enqueue(cid, 0) {
			queued++
		}
	}
	log.Printf("[scheduler] 📥 Успешно поставлено в очередь: %d из %d заведений", queued, len(companyIDs))
}


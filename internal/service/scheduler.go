package service

import (
	"log"
	"sync"
	"sync/atomic"
	"time"

	"qa2a/internal/repository"
)

// Scheduler управляет регулярным выполнением фоновых регламентных процедур:
// ежедневная ночная выгрузка списаний и перемещений в iiko RMS, ротация архива заявок.
type Scheduler struct {
	repo        *repository.Repository
	iikoSvc     *IikoService
	isRunning   atomic.Bool
	isExporting sync.Mutex
	quitChan    chan struct{}
	wg          sync.WaitGroup
}

// NewScheduler создает новый экземпляр планировщика фоновых задач.
func NewScheduler(repo *repository.Repository, iikoSvc *IikoService) *Scheduler {
	return &Scheduler{
		repo:     repo,
		iikoSvc:  iikoSvc,
		quitChan: make(chan struct{}),
	}
}

// Start запускает цикл фонового планировщика в отдельной горутине.
func (s *Scheduler) Start() {
	if !s.isRunning.CompareAndSwap(false, true) {
		log.Println("[scheduler] ⚠️ Планировщик уже запущен")
		return
	}

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

// scheduleLoop организует цикл ожидания до следующего запуска в 06:30 (МСК).
func (s *Scheduler) scheduleLoop() {
	defer s.wg.Done()

	// Загружаем Московскую таймзону (UTC+3)
	loc, err := time.LoadLocation("Europe/Moscow")
	if err != nil {
		log.Printf("[scheduler] ⚠️ Ошибка загрузки tz 'Europe/Moscow': %v. Используем фиксированное смещение +03:00", err)
		loc = time.FixedZone("MSK", 3*60*60)
	}

	for {
		now := time.Now().In(loc)

		// Расчет следующего окна запуска: сегодня 06:30:00
		nextRun := time.Date(now.Year(), now.Month(), now.Day(), 6, 30, 0, 0, loc)
		if now.After(nextRun) {
			nextRun = nextRun.Add(24 * time.Hour)
		}

		duration := time.Until(nextRun)
		log.Printf("[scheduler] ⏳ Следующая регламентная выгрузка в iiko запланирована на %s (через %v)",
			nextRun.Format("2006-01-02 15:04:05 MSK"), duration.Round(time.Minute))

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

		if err := s.iikoSvc.ExportDailyOperations(cid); err != nil {
			log.Printf("[scheduler] ⚠️ Сбой выгрузки для заведения #%d: %v", cid, err)
			failedCount++
		} else {
			successCount++
		}

		// Задержка 2 секунды между заведениями для предотвращения перегрузки серверов iiko
		time.Sleep(2 * time.Second)
	}

	elapsed := time.Since(startTime).Round(time.Second)
	log.Printf("[scheduler] 🏁 Регламентная выгрузка завершена за %v. Успешно: %d, ошибок: %d (всего: %d)",
		elapsed, successCount, failedCount, total)
}


package service

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"
)

// SyncTask представляет задачу синхронизации номенклатуры для конкретного заведения
type SyncTask struct {
	CompanyID int
	UserID    int
	Done      chan error
}

// SyncQueue управляет фоновой очередью синхронизации заведений с iiko RMS.
// Обеспечивает строгий Concurrency = 1 (одно заведение в один момент времени),
// дедупликацию задач и интервал охлаждения между обращениями к БД и внешним API.
type SyncQueue struct {
	iikoSvc      *IikoService
	queue        chan SyncTask
	inQueue      sync.Map // map[int]bool: companyID -> true
	isProcessing sync.Map // map[int]bool: companyID -> true
	stopChan     chan struct{}
	wg           sync.WaitGroup
	onceStart    sync.Once
	onceStop     sync.Once
}

// NewSyncQueue создает менеджер очереди с буфером указанного размера
func NewSyncQueue(iikoSvc *IikoService, bufferSize int) *SyncQueue {
	if bufferSize <= 0 {
		bufferSize = 100
	}
	return &SyncQueue{
		iikoSvc:  iikoSvc,
		queue:    make(chan SyncTask, bufferSize),
		stopChan: make(chan struct{}),
	}
}

// Start запускает рабочий цикл воркера очереди
func (q *SyncQueue) Start() {
	q.onceStart.Do(func() {
		q.wg.Add(1)
		go q.worker()
		log.Println("[sync-queue] 🚀 Очередь синхронизации номенклатуры активна (Concurrency: 1)")
	})
}

// Stop корректно завершает работу очереди
func (q *SyncQueue) Stop() {
	q.onceStop.Do(func() {
		close(q.stopChan)
		q.wg.Wait()
		log.Println("[sync-queue] 🛑 Очередь синхронизации номенклатуры остановлена")
	})
}

// Enqueue ставит заведение в очередь без ожидания. Если задача уже выполняется или в очереди — возвращает false.
func (q *SyncQueue) Enqueue(companyID int, userID int) bool {
	// Дедупликация: не ставим, если уже обрабатывается или ждет в очереди
	if _, busy := q.isProcessing.Load(companyID); busy {
		log.Printf("[sync-queue] ℹ️ Заведение #%d уже синхронизируется прямо сейчас, пропуск повторного добавления", companyID)
		return false
	}
	if _, exists := q.inQueue.LoadOrStore(companyID, true); exists {
		log.Printf("[sync-queue] ℹ️ Заведение #%d уже ожидает в очереди, пропуск дубликата", companyID)
		return false
	}

	select {
	case q.queue <- SyncTask{CompanyID: companyID, UserID: userID}:
		log.Printf("[sync-queue] 📥 Заведение #%d успешно поставлено в очередь", companyID)
		return true
	default:
		q.inQueue.Delete(companyID)
		log.Printf("[sync-queue] ⚠️ Буфер очереди переполнен, не удалось добавить заведение #%d", companyID)
		return false
	}
}

// EnqueueWait ставит задачу в очередь и блокирует вызов до завершения (или таймаута).
// Идеально для ручных вызовов из WebApp/MiniApp интерфейса.
func (q *SyncQueue) EnqueueWait(companyID int, userID int, timeout time.Duration) error {
	done := make(chan error, 1)
	task := SyncTask{
		CompanyID: companyID,
		UserID:    userID,
		Done:      done,
	}

	// Попытка зарегистрировать в очереди
	if _, busy := q.isProcessing.Load(companyID); busy {
		return fmt.Errorf("синхронизация заведения #%d уже выполняется в данный момент", companyID)
	}
	if _, exists := q.inQueue.LoadOrStore(companyID, true); exists {
		return fmt.Errorf("заведение #%d уже находится в очереди на синхронизацию", companyID)
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	select {
	case q.queue <- task:
		log.Printf("[sync-queue] 📥 Заведение #%d поставлено в очередь с ожиданием ответа", companyID)
	case <-ctx.Done():
		q.inQueue.Delete(companyID)
		return ctx.Err()
	}

	// Ждем завершения выполнения воркером
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		// Задача останется в очереди и выполнится в фоне, но клиенту вернем таймаут
		return ctx.Err()
	}
}

// worker последовательно извлекает задачи из очереди и выполняет синхронизацию
func (q *SyncQueue) worker() {
	defer q.wg.Done()

	for {
		select {
		case <-q.stopChan:
			return
		case task := <-q.queue:
			q.inQueue.Delete(task.CompanyID)
			q.isProcessing.Store(task.CompanyID, true)

			log.Printf("[sync-queue] ⚙️ Старт синхронизации номенклатуры заведения #%d...", task.CompanyID)
			startTime := time.Now()

			var err error
			func() {
				defer func() {
					if r := recover(); r != nil {
						err = fmt.Errorf("паника во время синхронизации: %v", r)
						log.Printf("[sync-queue] ❌ PANIC при синхронизации заведения #%d: %v", task.CompanyID, r)
					}
				}()
				err = q.iikoSvc.SyncNomenclature(task.CompanyID, task.UserID)
			}()

			duration := time.Since(startTime).Round(time.Millisecond)
			if err != nil {
				log.Printf("[sync-queue] ❌ Ошибка синхронизации заведения #%d (%v): %v", task.CompanyID, duration, err)
			} else {
				log.Printf("[sync-queue] ✅ Заведение #%d успешно синхронизировано за %v", task.CompanyID, duration)
			}

			if task.Done != nil {
				task.Done <- err
			}

			q.isProcessing.Delete(task.CompanyID)

			// Cooldown 2 секунды между заведениями для предотвращения всплесков нагрузки на Postgres и iiko
			select {
			case <-q.stopChan:
				return
			case <-time.After(2 * time.Second):
			}
		}
	}
}

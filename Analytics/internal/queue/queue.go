package queue

import (
	"database/sql"
	"fmt"
	"log"
	"sync"
	"time"
)

// SyncTask представляет задачу синхронизации накладных для заведения
type SyncTask struct {
	RestaurantID   int
	RestaurantName string
	From           string
	To             string
	Trigger        string // "daily_auto", "onboarding", "manual"
	Done           chan error
}

// SyncFn сигнатура функции синхронизации накладных из iiko RMS
type SyncFn func(restID int, from, to string) (int, int, error)

// SyncQueue управляет фоновой очередью синхронизации заведений с iiko RMS.
// Обеспечивает строгий Concurrency = 1 (одно заведение в один момент времени),
// дедупликацию задач и интервал охлаждения между обращениями к внешним API.
type SyncQueue struct {
	db           *sql.DB
	syncFn       SyncFn
	queue        chan SyncTask
	inQueue      sync.Map // map[int]bool: restID -> true
	isProcessing sync.Map // map[int]bool: restID -> true
	stopChan     chan struct{}
	wg           sync.WaitGroup
	onceStart    sync.Once
	onceStop     sync.Once
}

// NewSyncQueue создает менеджер очереди с буфером
func NewSyncQueue(db *sql.DB, syncFn SyncFn, bufferSize int) *SyncQueue {
	if bufferSize <= 0 {
		bufferSize = 100
	}
	return &SyncQueue{
		db:       db,
		syncFn:   syncFn,
		queue:    make(chan SyncTask, bufferSize),
		stopChan: make(chan struct{}),
	}
}

// Start запускает рабочий цикл воркера очереди
func (q *SyncQueue) Start() {
	q.onceStart.Do(func() {
		q.wg.Add(1)
		go q.worker()
		log.Println("🚀 [Analytics SyncQueue] Очередь автообновления накладных активна (Concurrency: 1)")
	})
}

// Stop корректно завершает работу очереди
func (q *SyncQueue) Stop() {
	q.onceStop.Do(func() {
		close(q.stopChan)
		q.wg.Wait()
		log.Println("🛑 [Analytics SyncQueue] Очередь автообновления накладных остановлена")
	})
}

// Enqueue ставит заведение в очередь без ожидания.
// Если задача уже выполняется или в очереди — возвращает false (дедупликация).
func (q *SyncQueue) Enqueue(task SyncTask) bool {
	if _, busy := q.isProcessing.Load(task.RestaurantID); busy {
		log.Printf("ℹ️ [SyncQueue] Заведение #%d (%s) уже синхронизируется прямо сейчас, пропуск", task.RestaurantID, task.RestaurantName)
		return false
	}
	if _, exists := q.inQueue.LoadOrStore(task.RestaurantID, true); exists {
		log.Printf("ℹ️ [SyncQueue] Заведение #%d (%s) уже ожидает в очереди, пропуск дубликата", task.RestaurantID, task.RestaurantName)
		return false
	}

	select {
	case q.queue <- task:
		log.Printf("📥 [SyncQueue] Заведение #%d (%s) поставлено в очередь [%s: %s -> %s]", task.RestaurantID, task.RestaurantName, task.Trigger, task.From, task.To)
		return true
	default:
		q.inQueue.Delete(task.RestaurantID)
		log.Printf("⚠️ [SyncQueue] Буфер очереди переполнен, не удалось добавить заведение #%d", task.RestaurantID)
		return false
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
			q.inQueue.Delete(task.RestaurantID)
			q.isProcessing.Store(task.RestaurantID, true)

			log.Printf("⚙️ [SyncQueue] Старт выгрузки накладных заведения #%d (%s) за период %s..%s...",
				task.RestaurantID, task.RestaurantName, task.From, task.To)
			startTime := time.Now()

			var importedInvoices, importedItems int
			var err error

			// Защита от паники во время обращения к iiko / парсинга XML / транзакций БД
			func() {
				defer func() {
					if r := recover(); r != nil {
						err = fmt.Errorf("паника во время синхронизации: %v", r)
						log.Printf("❌ [SyncQueue] PANIC при синхронизации заведения #%d: %v", task.RestaurantID, r)
					}
				}()

				// Повторные попытки при временных сетевых сбоях iiko RMS
				for attempt := 1; attempt <= 2; attempt++ {
					importedInvoices, importedItems, err = q.syncFn(task.RestaurantID, task.From, task.To)
					if err == nil {
						break
					}
					log.Printf("⚠️ [SyncQueue] Попытка %d для заведения #%d завершилась ошибкой: %v", attempt, task.RestaurantID, err)
					if attempt < 2 {
						time.Sleep(3 * time.Second)
					}
				}
			}()

			duration := time.Since(startTime).Round(time.Millisecond)

			// Фиксация статуса в таблице analytics_restaurants
			statusStr := "success"
			errStr := ""
			if err != nil {
				statusStr = "error"
				errStr = err.Error()
				log.Printf("❌ [SyncQueue] Ошибка синхронизации заведения #%d (%v): %v", task.RestaurantID, duration, err)
			} else {
				log.Printf("✅ [SyncQueue] Заведение #%d успешно синхронизировано за %v: накладных=%d, строк=%d",
					task.RestaurantID, duration, importedInvoices, importedItems)
			}

			if q.db != nil {
				_, _ = q.db.Exec(`
					UPDATE analytics_restaurants 
					SET last_synced_at = NOW(), 
					    last_sync_status = $1, 
					    last_sync_error = $2 
					WHERE id = $3`, statusStr, errStr, task.RestaurantID)
			}

			if task.Done != nil {
				task.Done <- err
			}

			q.isProcessing.Delete(task.RestaurantID)

			// Cooldown 2 секунды между заведениями для предотвращения всплесков нагрузки
			select {
			case <-q.stopChan:
				return
			case <-time.After(2 * time.Second):
			}
		}
	}
}

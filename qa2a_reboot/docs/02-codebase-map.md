# ЧАСТЬ 2: Карта кодовой базы (Directory Structure & Code Map)

## 2.1. Структура директорий сервиса QA2A (`/opt/qa2a-reboot/`)

```
/opt/qa2a-reboot/
├── cmd/
│   └── api/
│       └── main.go                 # Точка входа основного сервиса QA2A (DI, HTTP-сервер, роутинг)
├── fonts/                          # TrueType шрифты для генерации отчетов в PDF (Roboto-Regular)
├── internal/
│   ├── auth/
│   │   └── telegram.go             # Модуль криптографической верификации Telegram WebApp initData
│   ├── bot/
│   │   └── bot.go                  # Telegram-бот: рассылка алертов, бэкапов БД, обработка команд
│   ├── config/
│   │   └── config.go               # Парсинг и валидация конфигурации из .env и окружения ОС
│   ├── crypto/
│   │   └── crypto.go               # Симметричное шифрование AES-256 GCM с деривацией ключа PBKDF2
│   ├── database/
│   │   └── database.go             # Инициализация пула подключений к PostgreSQL (sqlx.DB)
│   ├── handlers/                   # Слой контроллеров (HTTP Transport Handlers)
│   │   ├── accounts.go             # Счета списаний (статьи расходов)
│   │   ├── auth.go                 # Авторизация Telegram WebApp, сессии
│   │   ├── company.go              # Управление заведением, инвайты, сотрудники
│   │   ├── handlers.go             # Базовая структура Handler, JSON-ответы, валидация компании
│   │   ├── iiko.go                 # Настройки подключения к iiko RMS, форсированный экспорт
│   │   ├── inventories.go          # Проведение и финализация инвентаризаций
│   │   ├── locations.go            # Управление складскими помещениями
│   │   ├── marketplace.go          # Получение офферов маркетплейса клиентами
│   │   ├── operations.go           # Списания, перемещения, согласование списаний смены
│   │   ├── procurements.go         # Заявки на закупку сырья
│   │   ├── supplier_portal.go      # Личный кабинет поставщика (офферы, регистрация)
│   │   ├── suppliers.go            # Контакты поставщиков и их привязка к Telegram
│   │   └── tickets.go              # Заявки в бухгалтерию (Service Desk)
│   ├── middleware/
│   │   └── auth.go                 # Middleware авторизации по Signed Token и проверки версий сессий
│   ├── models/
│   │   └── models.go               # Доменные сущности (Company, User, Operation, Inventory и др.)
│   ├── repository/                 # Слой доступа к данным (Data Access Layer / SQL-запросы)
│   │   ├── accounts.go             # Запросы к статьям расходов
│   │   ├── company.go              # Запросы к компаниям, членству, заявкам на вступление
│   │   ├── iiko.go                 # Настройки iiko, маппинги, справочники
│   │   ├── inventories.go          # Акты инвентаризации, строки пересчета, шаблоны
│   │   ├── locations.go            # Складские локации
│   │   ├── operations.go           # Движения остатков, очереди на согласование, агрегации
│   │   ├── procurements.go         # Заявки на закупку и их позиции
│   │   ├── repository.go           # Базовый репозиторий, транзакционный раннер (ExecuteInTx)
│   │   ├── suppliers.go            # Контакты и справочники контрагентов
│   │   ├── tickets.go              # Тикеты Service Desk
│   │   └── users.go                # Профили пользователей Telegram, token_version
│   └── service/                    # Слой бизнес-логики (Domain Services)
│       ├── auth.go                 # Логика прав доступа, смена ролей, инвайт-коды
│       ├── iiko.go                 # Интеграция с iiko RMS REST/OLAP API, экспорт документов
│       ├── inventory.go            # Расчет остатков, балансы, сверка инвентаризаций
│       ├── marketplace.go          # Управление предложениями поставщиков, клики, показы
│       ├── report.go               # Формирование сводных отчетов и печатных форм PDF
│       └── scheduler.go            # Регламентный планировщик ночной выгрузки списаний (06:30 YEKT)
├── migrations/                     # Исторические SQL-миграции схемы БД
├── pkg/
│   ├── netutil/
│   │   └── validator.go            # Валидация сетевых хостов и защита от SSRF-атак
│   └── ratelimit/
│       └── limiter.go              # In-memory Rate Limiter скользящего окна
├── web/
│   ├── static/
│   │   ├── css/                    # Стили интерфейса Telegram WebApp
│   │   └── js/
│   │       └── app.js              # Монолитное клиентское SPA-приложение Telegram WebApp
│   └── templates/
│       └── index.html              # HTML-контейнер для запуска Telegram Mini App
├── go.mod / go.sum                 # Модули Go и зависимости проекта
└── schema.sql                      # Полный DDL-манифест схемы базы данных PostgreSQL
```

---

## 2.2. Структура директорий сервиса парсинга (`/opt/iiko_parser/`)

```
/opt/iiko_parser/
├── ai_parser.go                    # Клиент к LLM API для извлечения данных из сканов и PDF накладных
├── auth.go                         # Аутентификация бухгалтеров (bcrypt, токены сессий, RBAC фирм)
├── crypto/
│   └── crypto.go                   # Симметричное AES-256 GCM шифрование паролей iiko RMS
├── db.go                           # Подключение к PostgreSQL, автомиграции и сидирование пресетов
├── handlers_accountant_invite.go   # Выпуск инвайтов для бухгалтеров и регистрация в фирме
├── handlers_analytics.go           # Анализ закупочных цен, токсичные списания, динамика инфляции
├── handlers_invite.go              # Привязка заведений к бухгалтерским фирмам по кодам
├── handlers_parser.go              # Загрузка файлов, запуск OCR/LLM, ручной маппинг, экспорт в iiko
├── handlers_presets.go             # Управление кастомными системными промптами парсера
├── handlers_reconciliation.go      # Парсинг и математическое сведение актов сверки взаиморасчетов
├── handlers_settings.go            # Настройки заведений, складов и синхронизация справочников
├── handlers_templates.go           # Создание бухгалтерских бланков инвентаризации и проксирование в QA2A
├── handlers_tickets.go             # Обработка обращений ресторанов (Service Desk), ответы, статусы
├── iiko_api.go                     # Низкоуровневый HTTP-клиент к iiko RMS REST API
├── main.go                         # Точка входа сервиса парсера, HTTP Mux (:8099), Graceful Shutdown
├── market_api.go                   # B2B Маркетплейс аналитика, арбитраж цен, досье поставщиков
├── models.go                       # Структуры данных накладных, позиций, парсинга и сверок
├── pkg/
│   ├── netutil/
│   │   └── validator.go            # Защита от SSRF при сетевых обращениях к серверам iiko
│   └── ratelimit/
│       └── limiter.go              # Rate Limiting попыток аутентификации и тяжелых запросов
├── prompts.go                      # Системные промпты для LLM (правила разбора УПД, чеков, Торг-12)
├── static/                         # Статический веб-интерфейс рабочего места бухгалтера
│   ├── index.html                  # Дашборд бухгалтера (Bugh-Team Workspace)
│   ├── market.html                 # Аналитический терминал маркетплейса и арбитража цен
│   └── js/
│       ├── analytics.js            # Графики цен, закупочная динамика
│       ├── auth.js                 # Авторизация в веб-интерфейсе
│       ├── main.js                 # Роутер и инициализация рабочей панели
│       ├── parser.js               # Интерфейс разбора накладных, таблица маппинга
│       ├── presets.js              # Редактор промптов парсинга
│       ├── reconciliation.js       # Интерфейс сведения актов сверки
│       ├── state.js                # Глобальное реактивное состояние UI
│       ├── templates.js            # Конструктор бланков инвентаризации
│       ├── tickets.js              # Интерфейс обработки тикетов заведений
│       └── ui.js                   # Модальные окна, уведомления, рендеринг таблиц
└── temp/                           # Временная директория для промежуточной обработки загружаемых файлов
```

---

## 2.3. Архитектурные слои и распределение ответственности

### 1. Слой представления и транспорта (Transport / Handlers)
* **QA2A**: `internal/handlers/*.go`. Принимают HTTP-запросы от Gorilla Mux, валидируют входные DTO, извлекают авторизованного пользователя через контекст (`middleware.GetUserID(ctx)`), проверяют доступ к заведению через `h.getCompanyID(r)` и делегируют задачи в сервисный слой. Форматируют унифицированные ответы через `respondJSON` и `respondError`.
* **iiko_parser**: `handlers_*.go`. Реализуют API для десктопного веб-интерфейса бухгалтера (JSON и Multipart формы). Проверяют Bearer-токен сессии бухгалтера и разграничивают доступ к ресторанам через `checkAccountantAccessUser`.

### 2. Слой доменной бизнес-логики (Domain / Service Layer)
* **`internal/service/auth.go`**: Бизнес-правила управления ролями, защита прав Владельца, генерация инвайт-кодов заведений.
* **`internal/service/inventory.go`**: Расчет списаний, валидация достаточного количества остатка, сведение фактических остатков ревизии с учетными остатками iiko OLAP.
* **`internal/service/iiko.go`**: Формирование XML-документов для iiko RMS (`writeoffDocument`, `transferDocument`, `inventoryDocument`), безопасное выполнение сетевых запросов через защищенный HTTP-клиент.
* **`internal/service/scheduler.go`**: Оркестрация регламентных ночных процедур: выгрузка неотправленных списаний в iiko RMS, ротация старых логов и вызов триггера резервного копирования БД.
* **`internal/service/marketplace.go`**: Управление каталогом поставщиков, поисковая индексация по тегам, трекинг просмотров и кликов ресторанов.
* **`iiko_parser/ai_parser.go`**: Формирование мультимодального промпта для LLM, отправка сканов/PDF, парсинг JSON, обработка ретраев и фолбэк-моделей.
* **`iiko_parser/market_api.go`**: Расчет индексов инфляции, выявление демпинга поставщиков, расчет арбитража цен на сырье.

### 3. Слой доступа к данным (Repository / Data Access)
* **`internal/repository/*.go`**: Инкапсулирует SQL-запросы к PostgreSQL через `jmoiron/sqlx`.
* **Транзакционность**: В `internal/repository/repository.go` реализован хелпер `ExecuteInTx(fn func(*sqlx.Tx) error)`, гарантирующий атомарность сложных операций (например, создание компании с первичным складом и привязкой владельца).
* **`iiko_parser/db.go`**: Прямой доступ к базе через стандартный драйвер `lib/pq` с пулом соединений.

### 4. Слой промежуточной обработки (Middleware)
* **`internal/middleware/auth.go`**:
  * `AuthMiddleware`: валидация криптографической подписи токена сессии `tgID:version:exp:sig` и сверка с `users.token_version`.
  * `SupplierAuthMiddleware`: контроль доступа поставщиков к личному кабинету маркетплейса.
* **`iiko_parser/auth.go`**:
  * `authMiddleware`: проверка Bearer-токена бухгалтера в таблице `accounting_users`.

### 5. Общие утилиты (Pkg)
* **`pkg/netutil/validator.go`**: Защита от SSRF и DNS-Rebinding атак при обращении к внешним серверам iiko RMS.
* **`pkg/ratelimit/limiter.go`**: In-memory скользящий ограничитель частоты запросов для защиты эндпоинтов авторизации от брутфорса.

---

## 2.4. Точки входа (Entry Points)

### QA2A Core API: `cmd/api/main.go`
1. Загрузка конфигурации из `.env` через `config.Load()`.
2. Инициализация пула соединений PostgreSQL (`database.NewWithConfig(cfg)`).
3. Dependency Injection: связывание репозитория, бизнес-сервисов и HTTP-обработчиков.
4. Запуск Telegram-бота администратора (`bot.New().Start()`).
5. Запуск фонового шедулера (`scheduler.New().Start()`) с хуком отправки дампов БД.
6. Конфигурация Gorilla Mux, CORS-политики, регистрация открытых и защищенных маршрутов.
7. Старт веб-сервера с таймаутами (`ReadHeaderTimeout: 10s`, `WriteTimeout: 60s`).
8. Graceful Shutdown: перехват сигналов SIGINT/SIGTERM, остановка шедулера, таймаут 15 сек на завершение активных запросов, закрытие соединений БД.

### iiko Parser Microservice: `iiko_parser/main.go`
1. Загрузка окружения `.env` и `/opt/qa2a-reboot/.env`.
2. Валидация критических переменных (`ENCRYPTION_KEY >= 32`, `EXTERNAL_API_KEY`, `AI_API_KEY`).
3. Подключение к PostgreSQL с пулом из 20 соединений.
4. Авто-миграция структуры таблицы `purchase_history`.
5. Сидирование пресетов промптов (`initPromptPresets`), суперадмина (`initRootSuperadmin`) и связок заведений.
6. Инициализация Safe HTTP Client с таймаутом 45 сек для iiko и 300 сек для LLM API.
7. Запуск HTTP Mux на порту `:8099` с поддержкой Graceful Shutdown.

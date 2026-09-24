# ЧАСТЬ 1: Высокоуровневая архитектура и Product Overview

## 1.1. Описание продукта и решаемые бизнес-задачи

**QA2A** — это распределенная модульная ERP-система складского учета, инвентаризации, бухгалтерского документооборота и B2B-закупок для ресторанного бизнеса (HoReCa: рестораны, бары, кофейни, фабрики-кухни) с прямой двусторонней интеграцией с ресторанным учетным сервером **iiko RMS**.

### Ключевые боли отрасли и решения QA2A:
1. **Человеческий фактор и разрыв между операционным контуром и бухгалтерией**: Повара и бармены забывают или физически не успевают фиксировать списания, перемещения и акты переработки на громоздких десктопных терминалах iiko. QA2A переносит учет в карман сотрудника через мобильный Telegram WebApp.
2. **Трудоемкий ввод первичных документов**: Бухгалтеры-калькуляторы тратят до 70% времени на ручной ввод бумажных УПД и ТОРГ-12 в iiko, допуская ошибки в фасовках, ценах и номенклатуре. Микросервис `iiko_parser` автоматизирует этот процесс с помощью мультимодальных LLM (Gemini / Claude / OpenAI API), распознающих сканы, PDF и Excel с расчетом коэффициентов пересчета (multiplier).
3. **Затянутая ночная инвентаризация**: Сверка фактических остатков затягивается из-за ручного сопоставления с книжными данными. QA2A на лету запрашивает расчетные остатки складов через iiko OLAP v2 API и сразу формирует интерактивную сличительную ведомость.
4. **Непрозрачность рынка и монополии поставщиков**: Рестораны переплачивают за сырье из-за отсутствия открытых рыночных бенчмарков. B2B Маркетплейс и модуль арбитража цен анализируют обезличенные закупки десятков заведений и выводят объективную медианную цену на каждое сырье.

---

## 1.2. Архитектурная схема взаимодействия компонентов

```mermaid
flowchart TB
    subgraph ClientLayer ["Клиентский уровень (Clients)"]
        TG_WA["Telegram WebApp (Mobile Client)<br><i>HTML5 / Vanilla JS / TG SDK</i>"]
        TG_BOT_CLIENT["Пользователь Telegram<br><i>(Шеф / Админ / Поставщик)</i>"]
        BUH_WEB["Bugh-Team Web Dashboard<br><i>(Рабочее место бухгалтера)</i>"]
        MKT_WEB["Marketplace Terminal<br><i>(B2B Витрина & Аналитика)</i>"]
    end

    subgraph GatewayAuth ["Входной шлюз и Безопасность"]
        TG_API["Telegram Bot API<br><i>(initData HMAC-SHA256)</i>"]
        CORS_SEC["Strict CORS & SSRF Firewall<br><i>(netutil/validator.go)</i>"]
    end

    subgraph CoreBackend ["Основной бэкенд QA2A (:8082)"]
        ROUTER_MAIN["HTTP Router (Gorilla Mux)"]
        AUTH_MW["Auth & Supplier Middleware<br><i>(Signed Token: tgID:ver:exp:sig)</i>"]
        SCHEDULER["Nightly Scheduler<br><i>(06:30 YEKT Auto-Export)</i>"]
        SERVICES_MAIN["Бизнес-сервисы<br><i>Auth | Inventory | Iiko | Mkt | Report</i>"]
        BOT_ENGINE["Admin Telegram Bot<br><i>(Алерты, Бэкапы, Уведомления)</i>"]
    end

    subgraph ParserService ["Микросервис iiko_parser (:8099)"]
        ROUTER_PARSER["HTTP Mux (:8099)"]
        AUTH_BUH["Accountant Auth & Session RBAC<br><i>(bcrypt / Bearer token)</i>"]
        AI_ENGINE["AI Parser Engine<br><i>(LLM: Gemini / Claude API)</i>"]
        RECON_ENGINE["Reconciliation Engine<br><i>(Акты сверки взаиморасчетов)</i>"]
        MARKET_ENGINE["Market Arbitrage Engine<br><i>(Анализ цен, инфляции, демпинга)</i>"]
    end

    subgraph DataStorage ["Слой хранения данных"]
        POSTGRES[("PostgreSQL 14+<br><i>Multi-tenant: companies / accounting_firms</i>")]
        FS_STORAGE["Локальное хранилище файлов<br><i>uploads/tickets | temp/</i>"]
    end

    subgraph ExternalIntegrations ["Внешние системы (External API)"]
        IIKO_RMS["iiko RMS Server (Ресторанный сервер)<br><i>REST API (/resto/api) + OLAP v2</i>"]
        LLM_API["LLM Provider Endpoint<br><i>(Gemini Flash / OpenAI-compatible)</i>"]
    end

    %% Связи клиентов
    TG_WA -->|HTTPS / API Requests| CORS_SEC
    TG_BOT_CLIENT <-->|Команды / Уведомления| TG_API
    BUH_WEB -->|REST API / Bearer Token| ROUTER_PARSER
    MKT_WEB -->|REST API| ROUTER_PARSER

    %% Авторизация
    CORS_SEC --> ROUTER_MAIN
    TG_API -.->|Верификация initData| ROUTER_MAIN
    ROUTER_MAIN --> AUTH_MW --> SERVICES_MAIN

    %% Взаимодействие сервисов
    SERVICES_MAIN <-->|EXTERNAL_API_KEY| ROUTER_PARSER
    BOT_ENGINE <--> TG_API

    %% Интеграция с БД
    SERVICES_MAIN -->|sqlx Connection Pool| POSTGRES
    ROUTER_PARSER -->|lib/pq Connection Pool| POSTGRES
    SERVICES_MAIN --> FS_STORAGE
    ROUTER_PARSER --> FS_STORAGE

    %% Интеграция с внешними сервисами
    SERVICES_MAIN -->|Safe HTTP Client (SSRF Protected)| IIKO_RMS
    ROUTER_PARSER -->|Safe HTTP Client (SSRF Protected)| IIKO_RMS
    AI_ENGINE -->|HTTP JSON / Multi-part| LLM_API
    SCHEDULER -->|Trigger Batch Export| SERVICES_MAIN
```

---

## 1.3. Спецификация компонентов экосистемы

| Компонент | Технологический стек | Порт / Хост | Зона ответственности |
| :--- | :--- | :--- | :--- |
| **QA2A Backend** (`/opt/qa2a-reboot`) | Go 1.22+, `gorilla/mux`, `jmoiron/sqlx`, `lib/pq` | `:8082` | Операционный складской учет, списания, согласование, инвентаризация, аутентификация через Telegram, регламентный шедулер, рассылка бэкапов. |
| **iiko_parser** (`/opt/iiko_parser`) | Go 1.22+, `net/http`, `golang.org/x/crypto`, `lib/pq` | `:8099` | Нейросетевое распознавание первичных документов (УПД/чеков), парсинг актов сверки, агрегация рыночных цен и B2B маркетплейс. |
| **Telegram WebApp** | HTML5, CSS3, Vanilla JS, Telegram SDK | Встроен в `:8082` | Мобильный SPA-интерфейс ресторана. Адаптирован под тач-скрины, работу с низким качеством сети (Wi-Fi склада/подвала). |
| **Telegram Bot API** | HTTPS Long Polling / Webhook | `api.telegram.org` | Канал верификации цифровых подписей, рассылка алертов управляющим о заявках на вход и списаниях смены, доставка ночных дампов БД. |
| **База данных PostgreSQL** | PostgreSQL 14+ | `:5433` (или `:5432`) | Единый реляционный кластер с логической изоляцией заведений (`company_id`) и бухгалтерских компаний (`accounting_firm_id`). |
| **Внешний iiko RMS** | REST API XML/JSON, OLAP v2 | Внешний IP заведения | Ресторанная ERP: получение справочников номенклатуры, складов, статей списания и выгрузка актов прихода/расхода/инвентаризации. |

---

## 1.4. Детальная ролевая модель (RBAC)

Система реализует трехуровневую иерархию прав доступа:
1. **Персонал заведения** (`memberships.role`): `owner`, `admin`, `manager`, `user`.
2. **Поставщики** (`marketplace_supplier_users.role`): `admin`, `rep`.
3. **Бухгалтерский контур** (`accounting_users.role`): `superadmin`, `global_accountant`, `head`, `accountant`.

### Матрица прав доступа к операциям

| Операция / Раздел | User | Manager | Admin | Owner | Accountant | Superadmin |
| :--- | :---: | :---: | :---: | :---: | :---: | :---: |
| **Создание списания / перемещения** | ✅ | ✅ | ✅ | ✅ | ❌ | ✅ |
| **Согласование списаний смены (Approval)** | ❌ | ✅ | ✅ | ✅ | ❌ | ✅ |
| **Создание складов и позиций вручную** | ❌ | ✅ | ✅ | ✅ | ❌ | ✅ |
| **Проведение инвентаризации (ввод факта)** | ✅ | ✅ | ✅ | ✅ | ❌ | ✅ |
| **Финализация ревизии и экспорт в iiko** | ❌ | ❌ | ✅ | ✅ | ✅ | ✅ |
| **Настройки подключения к iiko RMS** | ❌ | ❌ | ✅ | ✅ | ❌ | ✅ |
| **Изменение ролей сотрудников** | ❌ | ❌ | ⚠️ *(кроме Owner)* | ✅ | ❌ | ✅ |
| **Удаление сотрудников из заведения** | ❌ | ❌ | ⚠️ *(только User)* | ✅ | ❌ | ✅ |
| **Парсинг накладных и экспорт в iiko** | ❌ | ❌ | ❌ | ❌ | ✅ | ✅ |
| **Разбор неучтенных товаров (Ghost Items)** | ❌ | ❌ | ❌ | ❌ | ✅ | ✅ |
| **Сверка актов взаиморасчетов (Reconciliation)** | ❌ | ❌ | ❌ | ❌ | ✅ | ✅ |
| **Доступ к арбитражу цен и рынку (Market)** | ❌ | ❌ | ❌ | ❌ | ❌ | ✅ |
| **Управление бухгалтерскими фирмами** | ❌ | ❌ | ❌ | ❌ | ❌ | ✅ |

> **Критическое правило безопасности (`internal/service/auth.go`)**:
> Действующий Владелец (`owner`) заведения защищен от понижения в правах и удаления любыми администраторами. Передать права Владельца может только сам текущий Владелец.

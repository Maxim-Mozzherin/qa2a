# 📚 Техническая документация экосистемы QA2A (Source of Truth)

Добро пожаловать в единую техническую документацию платформы **QA2A** и микросервиса **iiko_parser**. Данный репозиторий знаний предназначен для основателей, технических лидеров и разработчиков, работающих над расширением и эксплуатацией платформы.

---

## 📑 Оглавление и навигация по разделам

Документация разбита на 6 логических частей Enterprise-уровня:

| Раздел | Файл документации | Ключевые темы |
| :--- | :--- | :--- |
| **ЧАСТЬ 1** | [01-architecture-overview.md](./01-architecture-overview.md) | Описание продукта, решаемые бизнес-задачи HoReCa, общая архитектурная схема (Mermaid), спецификация сервисов и ролевая модель (RBAC: Owner, Admin, Manager, User, Supplier, Superadmin). |
| **ЧАСТЬ 2** | [02-codebase-map.md](./02-codebase-map.md) | Карта директорий `qa2a-reboot` и `iiko_parser`, назначение слоев (Handlers, Service, Repository, Middleware, Pkg, Web/Static), детальный разбор точек входа `main.go`. |
| **ЧАСТЬ 3** | [03-security-and-auth.md](./03-security-and-auth.md) | Механизм авторизации через Telegram WebApp `initData`, устройство Signed Tokens и `token_version`, симметричное шифрование паролей iiko (AES-256 GCM + PBKDF2), защита от SSRF и DNS-Rebinding (`netutil/validator.go`). |
| **ЧАСТЬ 4** | [04-database-schema.md](./04-database-schema.md) | Схема базы данных PostgreSQL 14+, диаграмма связей сущностей (ERD), реализация мультиарендности (`company_id` и `accounting_firm_id`), описание всех таблиц и структур. |
| **ЧАСТЬ 5** | [05-business-flows.md](./05-business-flows.md) | Сквозные бизнес-алгоритмы: флоу AI-парсинга накладных и расчет `multiplier`, проведение инвентаризации с OLAP iiko, регламентная ночная выгрузка списаний (06:30 YEKT), разбор неучтенки (Ghost Items) и B2B маркетплейс. |
| **ЧАСТЬ 6** | [06-infrastructure-and-tech-debt.md](./06-infrastructure-and-tech-debt.md) | Переменные окружения `.env`, многопоточность и фоновые горутины, Rate Limiting, честный аудит технического долга и план оптимизаций перед масштабированием. |

---

## 🛠 Быстрый старт разработчика

### Сборка и запуск локально:
```bash
# 1. Запуск основного бэкенда QA2A
cd qa2a-reboot
go build -o qa2a ./cmd/api
./qa2a

# 2. Запуск сервиса парсинга накладных
cd ../iiko_parser
go build -o iiko-parser .
./iiko-parser
```

### Проверка статусов сервисов на прод-сервере:
```bash
systemctl status qa2a.service iiko-parser.service --no-pager
journalctl -u qa2a.service -f
journalctl -u iiko-parser.service -f
```

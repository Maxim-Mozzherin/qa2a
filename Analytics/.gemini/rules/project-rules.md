# QA2A Project Rules & Ecosystem Guide

## 1. Resource Access Priorities

### Files
- **READ/WRITE locally** via Samba mount. SSH for file I/O is FORBIDDEN.
- Path mapping:
  - `Z:\qa2a_reboot\` / `Z:\qa2a-reboot\` = `/opt/qa2a-reboot/` (Core WebApp / Marketplace)
  - `Z:\iiko_parser\` = `/opt/iiko_parser/` (Invoice Parser & iiko RMS integration)
  - `Z:\Analytics\` = `/opt/Analytics/` (Reverse Procurement Analytics Service)
  - `D:\My Drive\qa2a-reboot\` = project repo (Google Drive backup)
- Use `view_file`, `write_to_file`, `replace_file_content` with `Z:\...` paths.

### Database
- **ALWAYS** use MCP tool `postgres/query`. No SSH+psql, no docker exec, no Go scripts for queries unless debugging containers.
- Tunnel for MCP: `127.0.0.1:5433` -> `5432` on `2.26.106.236`.

### SSH — server ops only
- Allowed: `go build`, `systemctl restart`, `nginx`, `docker`, network checks.
- Forbidden: editing source code via nano/vim, dumping unbounded logs, running manual psql queries.

---

## 2. Token Economy — Hard Output Limits

| Action | NEVER | ALWAYS |
|--------|-------|--------|
| Logs | `cat`, raw `journalctl`, raw `docker logs` | `tail -n 30`, `journalctl -u <unit> -n 30 --no-pager`, `docker logs --tail 30` |
| Search | Dump entire file | `grep -n "term" file` or `grep -A5 -B5` |
| Read code | `cat` files >30 lines | `sed -n 'X,Yp'` — but prefer local Samba read |
| JSON/API | Raw curl output | `curl -s ... \| jq .field` or `head -n 20` |
| Install | Verbose output | `-q`, `--silent`, `> /dev/null 2>&1` |
| Responses | Repeat large code blocks | Reference file + line number, show only modified diff |

---

## 3. Server Infrastructure & Services

### Connection
- Host: `2.26.106.236`, user: `root`, pass: `!123Max.,!`
- PostgreSQL credentials: user `admin`, pass `!123Maxim.!`, database `qa2a`, port `5433` (container `qa2a-postgres`).

### Running Services
| Service | Port | Path | Systemd Unit | Binary | Primary Purpose |
|---------|------|------|--------------|--------|-----------------|
| **QA2A Backend** | 8082 | `/opt/qa2a-reboot` | `qa2a` | `qa2a` | Telegram WebApp, marketplace, supplier bids |
| **iiko Parser** | 8099 | `/opt/iiko_parser` | `iiko_parser` | `iiko_parser` | Parsing iiko XML invoices, accountant dashboard |
| **Analytics Service** | 8098 | `/opt/Analytics` | `analytics` | `analytics-server` | Reverse procurement audit, price radar, executive reports |

### SSH Tunnel for MCP PostgreSQL
Before using `postgres/query`, verify tunnel is active:
```powershell
Test-NetConnection -ComputerName 127.0.0.1 -Port 5433
```
If `TcpTestSucceeded: False`, launch tunnel in background:
```powershell
ssh -L 5433:127.0.0.1:5433 -N -o StrictHostKeyChecking=no root@2.26.106.236
```

### Build & Deploy Commands
```bash
# 1. QA2A Core
cd /opt/qa2a-reboot && go build -o qa2a ./cmd/api && systemctl restart qa2a

# 2. iiko Parser
cd /opt/iiko_parser && go build -o iiko_parser . && systemctl restart iiko_parser

# 3. Analytics Service
cd /opt/Analytics && go build -o analytics-server ./cmd/analytics_server && systemctl restart analytics
```

---

## 4. Project Structure

```
/opt/qa2a-reboot/ (Z:\qa2a_reboot\ and Z:\qa2a-reboot\)
  cmd/api/main.go               — entrypoint, migrations, router
  internal/
    config/config.go            — config, DSN
    handlers/                   — API handlers (auth, parser, market)
    service/                    — business logic (iiko export, marketplace, suppliers)
  web/                          — frontend SPA
  schema.sql                    — database schema

/opt/iiko_parser/ (Z:\iiko_parser\)
  main.go                       — entrypoint
  auth.go                       — accounting_users auth logic & tokens
  static/                       — parser UI, JavaScript & CSS
  schema.sql                    — parser schema & accounting tables

/opt/Analytics/ (Z:\Analytics\)
  cmd/analytics_server/main.go  — service entrypoint & routing
  internal/
    api/                        — modular HTTP handlers & server
      server.go                 — server context & routing
      handlers_auth.go          — login, logout, /api/analytics/me
      handlers_restaurants.go   — restaurant listing, metrics, onboarding
      handlers_sync.go          — invoice sync from iiko RMS
      handlers_audit.go         — core executive financial audit calculation
      middleware.go             — RequireSuperAdmin RBAC middleware
    auth/                       — session & super-admin security
      auth.go                   — auth against accounting_users, bcrypt, session cookies
    engine/                     — financial calculations & modeling
      finance.go                — ExecutiveFinancialAudit, Freemium mask, HHI, dual overpayments
    taxonomy/                   — modular HoReCa raw material classification
      normalizer.go             — text sanitization & pipeline orchestrator
      dairy.go                  — cream (33%, 20%, 10%), milk, butter, cheese, vegetable (ЗМЖ)
      meat_fish.go              — squid, salmon, poultry, beef, seafood
      groceries.go              — starch (corn/potato), flour, oils, vegetables
      brands.go                 — brand extraction (Чудское озеро, Петмол, Parmalat, etc.)
    llm/                        — executive auditor narrative generation
      summary.go                — zero-hallucination LLM summary & deterministic fallback
    db/                         — database connection pool
      db.go
    config/                     — environment variables loader
      config.go
  web/
    analytics.html              — executive web dashboard & A4/PDF print report modal
    login.html                  — super-admin authentication page
```

---

## 5. Analytics Service: Business Rules & Constraints

### 5.1. Diplomatic Terminology & Strict Neutrality
- **NEVER name or single out specific supplier companies in recommendations or executive summaries.**
  - FORBIDDEN: «Провести переговоры с ООО РЕМО...»
  - REQUIRED: «Провести плановую актуализацию коммерческих условий и объемных спецификаций с ключевыми поставщиками.»
- **Corporate Audit Lexicon:**
  - Deprecated: "Индекс двуличия" / "Supplier Discrimination" -> **`SupplierPriceSpread` / Внутрипоставочный ценовой спред**.
  - Deprecated: "Налог на лень" / "Кривые руки" -> **`OffContractSpend` / Издержки оперативных (розничных) закупок**.

### 5.2. Dual Overpayment Metrics
Always compute and display both overpayment benchmarks simultaneously:
1. **К средней рынка (базовая норма):** `total_monthly_overpay_vs_avg_rub` / `monthly_overpay_vs_avg_rub`  
   *Консервативный срез* — справедливый перерасход сверх рыночной нормы независимых заведений того же объема.
2. **К минимуму когорты (максимальный потенциал):** `total_monthly_overpay_vs_min_rub` / `monthly_overpay_vs_min_rub`  
   *Агрессивный срез* — максимальный резерв экономии при переходе на лучшие городские контракты.
*Constraint:* All numbers across cards, data tables, upsell teasers, and textual audit summaries must strictly match to the ruble and tenth of a percent.

### 5.3. Freemium Masking & Paywall Logic
- In demo mode (`mode=demo` or default):
  - Top 3 overpaid items (Rank 1, 2, 3) are fully revealed with raw suppliers, prices, and breakdowns.
  - Items from Rank 4 onwards are obfuscated: product name replaced with `[Скрыто в демо-версии]`, supplier replaced with `Скрытый поставщик`, breakdowns and peer lists set to `nil`.
  - All numerical financial fields (`monthly_overpay_rub`, `monthly_overpay_vs_min_rub`, volume, tier, prices) are strictly preserved so UI displays real economic totals.
- Root aggregate metrics:
  - `revealed_items_overpay_rub` & `revealed_items_overpay_vs_min_rub` (Top-3 items)
  - `hidden_items_overpay_rub` & `hidden_items_overpay_vs_min_rub` (Rank 4+ items)
  - `hidden_positions_count`

### 5.4. No Emojis in Print Reports
- Inside `#report-document-sheet` (A4 / PDF print document), **all emojis and icons are strictly forbidden** (`🤖`, `🔹`, `⭐`, `💎`, `📈`, `🍩`, `🔒`, `⚠️`, `📥`, `📋`, `🗂️`).
- Visuals must rely solely on corporate typography, subtle borders, and professional audit badges.

### 5.5. No "AI" Branding in User-Facing UI
- Block header: **«Экспертное заключение управленческого аудитора»**.
- Subtitle: **«Экспертный аналитический отчет на основе детерминированных финансовых метрик закупок»**.
- Never display `AI Executive Summary`, `AI`, or robot badges to end users.

### 5.6. Taxonomy & Product Classification
- Classification must NEVER be crammed into a single monolithic file. Always keep it modular under `internal/taxonomy/`.
- **Dairy Separation:**
  - Natural whipping cream 33–35% (`Сливки 33-35% (для взбивания)`)
  - Cooking cream 20–22% (`Сливки 20-22% (кулинарные)`)
  - Drinking/coffee cream 10–11% (`Сливки 10-11% (питьевые/кофейные)`)
  - Spray/aerosol cream (`Сливки взбитые аэрозольные (спрей/баллон)`)
  - Vegetable / ZMH cream (`Крем растительный для взбивания (ЗМЖ) / Шантипак`)
- **Starch Separation:**
  - `Крахмал кукурузный` vs `Крахмал картофельный`

### 5.7. Volume Cohort Principles
- Default thresholds: Small (`< 50 кг/мес`), Medium (`50 – 200 кг/мес`), Large (`> 200 кг/мес`).
- Pure market benchmark: Market statistics (avg, median, min, P25, P75) are computed **strictly excluding the target restaurant itself**.

---

## 6. Authentication & Super-Admin RBAC

- User accounts are stored in `accounting_users` table in PostgreSQL.
- Access to `/analytics` view and `/api/analytics/*` endpoints strictly requires `is_superuser == true` (role = `'superadmin'`).
- Validated via middleware `RequireSuperAdmin` checking:
  1. `Authorization: Bearer <access_token>`
  2. HTTP-only session cookie `analytics_session`
  3. Parser session cookie `access_token`

---

## 7. Standard Workflow for Changes

1. **Local edit via Samba mount (`Z:\...`)** — fast, transparent, preserves encoding.
2. **Compile on server via SSH:**
   - QA2A: `cd /opt/qa2a-reboot && go build -o qa2a ./cmd/api && systemctl restart qa2a`
   - Parser: `cd /opt/iiko_parser && go build -o iiko_parser . && systemctl restart iiko_parser`
   - Analytics: `cd /opt/Analytics && go build -o analytics-server ./cmd/analytics_server && systemctl restart analytics`
3. **Verify status:** `systemctl status <unit> --no-pager`
4. **Test endpoints:** via curl with Bearer token or browser.

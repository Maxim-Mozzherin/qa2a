# QA2A Project Rules

## 1. Resource Access Priorities

### Files
- **READ/WRITE locally** via Samba mount. SSH for file I/O is FORBIDDEN.
- Path mapping:
  - `Z:\qa2a-reboot\` = `/opt/qa2a-reboot/`
  - `Z:\iiko_parser\` = `/opt/iiko_parser/`
  - `D:\My Drive\qa2a-reboot\` = project repo (Google Drive)
- Use `view_file`, `write_to_file`, `replace_file_content` with Z: paths.

### Database
- **ALWAYS** use MCP tool `postgres/query`. No SSH+psql, no docker exec, no Go scripts for queries.

### SSH — server ops only
Allowed: build, restart, nginx, systemd, certbot, iptables, docker start/stop, network checks.
Forbidden: reading/writing source code, dumping logs without limits, running psql.

---

## 2. Token Economy — Hard Output Limits

| Action | NEVER | ALWAYS |
|--------|-------|--------|
| Logs | `cat`, raw `journalctl`, raw `docker logs` | `tail -n 30`, `journalctl -n 30 --no-pager`, `docker logs --tail 30` |
| Search | Dump entire file | `grep -n "term" file` or `grep -A5 -B5` |
| Read code | `cat` files >30 lines | `sed -n 'X,Yp'` — but prefer local Samba read |
| JSON/API | Raw curl output | `curl -s ... \| jq .field` or `head -n 20` |
| Install | Verbose output | `-q`, `--silent`, `> /dev/null 2>&1` |
| Responses | Repeat large code blocks | Reference file + line number, show only diff |

---

## 3. Server Infrastructure

### Connection
- IP: `2.26.106.236`, user: `root`, pass: `!123Max.,!`
- SSH via Go script with `golang.org/x/crypto/ssh` (PowerShell mangles special chars).

### Services
| Service | Port | Path | Unit |
|---------|------|------|------|
| QA2A Backend | 8082 | /opt/qa2a-reboot | `qa2a` |
| iiko Parser | 8099 | /opt/iiko_parser | `iiko_parser` |

### SSH Tunnel for MCP PostgreSQL
Before using `postgres/query`, verify tunnel is active:
```powershell
Test-NetConnection -ComputerName 127.0.0.1 -Port 5433
```
If `TcpTestSucceeded: False`, start tunnel as daemon:
```powershell
ssh -L 5433:127.0.0.1:5433 -N -o StrictHostKeyChecking=no root@2.26.106.236
```
Then send password `!123Max.,!` via `send_input`.

### Build & Deploy
```bash
# QA2A
cd /opt/qa2a-reboot && go build -o qa2a ./cmd/api && systemctl restart qa2a
# iiko Parser
cd /opt/iiko_parser && go build -o iiko_parser . && systemctl restart iiko_parser
```

### Database
- Container: `qa2a-postgres` (postgres:16-alpine)
- Port: `127.0.0.1:5433` -> `5432`
- User: `admin`, pass: `!123Maxim.!`
- Databases: `qa2a` (main), `vpn_manager`, `kidquest`

---

## 4. Project Structure

```
/opt/qa2a-reboot/ (Z:\qa2a-reboot\)
  cmd/api/main.go             — entrypoint, migrations, router
  internal/
    config/config.go          — config, DSN
    handlers/
      auth.go                 — Telegram WebApp auth
      handlers_parser.go      — invoice parsing, iiko XML
      market_api.go           — marketplace API
    service/
      auth.go                 — auth logic, roles
      iiko.go                 — iiko integration, nightly export
      marketplace.go          — marketplace, supplier offers
      supplier.go             — supplier logic
  web/
    templates/index.html      — main HTML (SPA)
    static/js/app.js          — frontend logic
  schema.sql                  — full DB schema

/opt/iiko_parser/ (Z:\iiko_parser\)
  main.go                     — entrypoint
  auth.go                     — parser auth
  static/
    index.html                — parser UI
    js/main.js                — main UI logic
    js/parser.js              — file upload & parsing
  schema.sql                  — parser DB schema
```

---

## 5. Code Standards

### Go
- SQL empty string: `!= ''` (two single quotes), never `!= ""`
- API JSON: standard double quotes `{"status": "ok"}`
- iiko XML: escape with `<![CDATA[%s]]>`
- Batch loops: on item error -> `log.Printf` + `continue`, NEVER `return fmt.Errorf`
- User-facing strings: Russian language

### JavaScript
- All alert() messages in Russian
- One `DOMContentLoaded` listener per file, no duplicates

### SQL / Migrations
- New tables/columns -> add to `schema.sql`
- Migrations in `main.go` -> use `IF NOT EXISTS` / `ADD COLUMN IF NOT EXISTS`

---

## 6. Workflow

### Code changes
1. Edit files locally via Z: (Samba path)
2. Files sync to server automatically
3. SSH: `go build` + `systemctl restart` only
4. Verify: `systemctl status <unit> --no-pager -l | head -15`

### Schema changes
1. Write ALTER/CREATE, execute via MCP `postgres/query`
2. Add migration to `schema.sql` and `main.go` (initDB)

### Debugging
1. Logs: `journalctl -u <svc> -n 30 --no-pager`
2. DB: MCP `postgres/query`
3. HTTP: `curl -s -o /dev/null -w "%{http_code}" <url>` or `curl -s <url> | jq .field`

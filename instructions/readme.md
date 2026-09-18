# 🚀 QA2A & IIKO Parser — База Знаний

Добро пожаловать в рабочую документацию проекта QA2A. Здесь собраны все необходимые доступы, команды и структура проекта. 

## 📁 Структура на сервере
Основная рабочая директория: `/opt/`

* `/opt/qa2a-reboot/` — Основной бэкенд проекта (Telegram-боты, авторизация, бизнес-логика).
* `/opt/iiko_parser/` — Микросервис парсинга накладных через ИИ (Claude) и раздел аналитики рынка (Godmode / Market).

> **Важно:** Папка `/opt/` сама по себе является Git-репозиторием, который привязан к GitHub.

## 🔑 Доступы и Кредиты
* **SSH Server (IP):** `2.26.106.236`
* **SSH Пользователь:** `root`
* **SSH Пароль:** `!123Max.,!`
* **GitHub Token:** `[CONFIGURED_IN_GIT]`
  *(Токен классического типа с полными правами `repo`)*

> Токен уже прописан в настройках Git на этом сервере.

## 🛠 Частые команды (Шпаргалка)

### Сборка и перезапуск основного бэкенда (QA2A)
```bash
cd /opt/qa2a-reboot
go build -o qa2a ./cmd/api
systemctl restart qa2a.service
```

### Сборка и перезапуск парсера (iiko_parser)
```bash
cd /opt/iiko_parser
go build -o iiko-parser
systemctl restart iiko-parser.service
```

### Мониторинг служб (Логи и Статус)
```bash
# Проверить статус, чтобы убедиться, что они работают:
systemctl status qa2a.service iiko-parser.service --no-pager

# Посмотреть логи парсера в реальном времени:
journalctl -u iiko-parser.service -f

# Посмотреть логи основного приложения:
journalctl -u qa2a.service -f
```

### Git (Как обновить код в GitHub)
Так как код лежит прямо в `/opt/`, команды для отправки кода на гитхаб выполняются оттуда же.
```bash
cd /opt
# Скачиваем новые изменения, если кто-то правил код через сайт GitHub
git pull origin main --no-edit

# Добавляем все нужные изменения из папок проектов:
git add iiko_parser qa2a-reboot

# Делаем коммит
git commit -m "Опишите ваши изменения здесь"

# Отправляем на GitHub
git push origin main
```


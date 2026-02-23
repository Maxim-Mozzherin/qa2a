package main

import (
    "crypto/hmac"
    "crypto/sha256"
    "database/sql"
    "encoding/hex"
    "fmt"
    "net/http"
    "net/url"
    "sort"
    "strings"
    "time"

    _ "github.com/lib/pq"
    "github.com/go-chi/chi/v5"
    "github.com/wesleym/telegramwidget"
)

var db *sql.DB
var botToken string

func main() {
    botToken = "8364435346:AAHoKylC6rhKsvWqP6Qp-IoAIqQBPOqfZSA" // Замените
    // Подключение к БД (из env в Docker)
    connStr := "host=postgres user=admin dbname=qa2a password=12332145 sslmode=disable"
    var err error
    db, err = sql.Open("postgres", connStr)
    if err != nil { panic(err) }
    defer db.Close()
    db.Ping()

    r := chi.NewRouter()
    r.Get("/", homeHandler)
    r.Post("/auth", authHandler)
    http.ListenAndServe(":8080", r)
}

func homeHandler(w http.ResponseWriter, r *http.Request) {
    http.ServeFile(w, r, "index.html")
}

func authHandler(w http.ResponseWriter, r *http.Request) {
    if err := r.ParseForm(); err != nil {
        http.Error(w, "Bad request", 400)
        return
    }
    user, err := telegramwidget.ConvertAndVerifyForm(r.Form, botToken)
    if err != nil {
        http.Error(w, "Auth failed", 401)
        return
    }
    // Сохранить в БД
    _, err = db.Exec("INSERT INTO users (telegram_id, username, first_name) VALUES ($1, $2, $3) ON CONFLICT (telegram_id) DO NOTHING", user.ID, user.Username, user.FirstName)
    if err != nil { fmt.Println(err) }
    // Установить сессию (cookie)
    http.SetCookie(w, &http.Cookie{Name: "session", Value: fmt.Sprintf("%d", user.ID)})
    http.Redirect(w, r, "/dashboard", 302)
}


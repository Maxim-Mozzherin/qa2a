package main

import (
	"log"
	"net/http"
	"github.com/gorilla/mux"
	"qa2a/internal/config"
	"qa2a/internal/database"
	"qa2a/internal/repository"
	"qa2a/internal/service"
	"qa2a/internal/handlers"
)

func main() {
	cfg, err := config.Load()
	if err != nil { log.Fatal(err) }

	db, err := database.New(cfg.DSN())
	if err != nil { log.Fatal(err) }
	defer db.Close()

	repo := repository.New(db)
	svc := service.NewAuthService(repo)
	h := handlers.New(svc)

	r := mux.NewRouter()
	r.HandleFunc("/api/auth", h.AuthHandler).Methods("POST")

	log.Println("🚀 Server started on :8080")
	log.Fatal(http.ListenAndServe(":8080", r))
}

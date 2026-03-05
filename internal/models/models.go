package models

import "time"

type Company struct {
	ID        int       `json:"id" db:"id"`
	Name      string    `json:"name" db:"name"`
	CreatedAt time.Time `json:"created_at" db:"created_at"`
}

type User struct {
	ID        int       `json:"id" db:"id"`
	TgID      int64     `json:"tg_id" db:"tg_id"`
	Username  string    `json:"username" db:"username"`
	FullName  string    `json:"full_name" db:"full_name"`
	CreatedAt time.Time `json:"created_at" db:"created_at"`
}

type Membership struct {
	UserID    int    `json:"user_id" db:"user_id"`
	CompanyID int    `json:"company_id" db:"company_id"`
	Role      string `json:"role" db:"role"`
}

type Operation struct {
	ID           int       `json:"id" db:"id"`
	CompanyID    int       `json:"company_id" db:"company_id"`
	UserID       int       `json:"user_id" db:"user_id"`
	Type         string    `json:"type" db:"type"`
	PositionName string    `json:"position_name" db:"position_name"`
	Quantity     float64   `json:"quantity" db:"quantity"`
	Unit         string    `json:"unit" db:"unit"`
	Status       string    `json:"status" db:"status"`
	CreatedAt    time.Time `json:"created_at" db:"created_at"`
}

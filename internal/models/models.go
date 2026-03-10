package models

import (

	"time"
)

type Company struct {
	ID         int       `json:"id" db:"id"`
	Name       string    `json:"name" db:"name"`
	InviteCode string    `json:"invite_code" db:"invite_code"`
	CreatedAt  time.Time `json:"created_at" db:"created_at"`
}

type User struct {
	ID        int       `json:"id" db:"id"`
	TgID      int64     `json:"tg_id" db:"tg_id"`
	Username  string    `json:"username" db:"username"`
	FullName  string    `json:"full_name" db:"full_name"`
	CreatedAt time.Time `json:"created_at" db:"created_at"`
}

type Membership struct {
	UserID      int    `json:"user_id" db:"user_id"`
	CompanyID   int    `json:"company_id" db:"company_id"`
	CompanyName string `json:"company_name" db:"company_name"`
	UserName    string `json:"user_name" db:"user_name"`
	Role        string `json:"role" db:"role"`
	CustomTitle string `json:"custom_title" db:"custom_title"` 
}

type Operation struct {
	ID           int       `json:"id" db:"id"`
	CompanyID    int       `json:"company_id" db:"company_id"`
	LocationID   int       `json:"location_id" db:"location_id"` // <-- ДОБАВИЛИ
	UserID       int       `json:"user_id" db:"user_id"`
	UserName     string    `json:"user_name" db:"user_name"`
	Type         string    `json:"type" db:"type"`
	PositionName string    `json:"position_name" db:"position_name"`
	Quantity     float64   `json:"quantity" db:"quantity"`
	Unit         string    `json:"unit" db:"unit"`
	Status       string    `json:"status" db:"status"`
	CreatedAt    time.Time `json:"created_at" db:"created_at"`
	IsUnlisted   bool      `json:"is_unlisted" db:"is_unlisted"`
	Comment 	 string    `json:"comment" db:"comment"`
}

type Balance struct {
	CompanyID    int     `json:"company_id" db:"company_id"`
	LocationID   int     `json:"location_id" db:"location_id"` // <-- ДОБАВИЛИ
	PositionName string  `json:"position_name" db:"position_name"`
	Quantity     float64 `json:"quantity" db:"quantity"`
	Unit         string  `json:"unit" db:"unit"`
}

type Position struct {
	ID        int    `json:"id" db:"id"`
	CompanyID int    `json:"company_id" db:"company_id"`
	Name      string `json:"name" db:"name"`
	Unit      string `json:"unit" db:"unit"`
	Supplier  string `json:"supplier" db:"supplier"`
}

type Location struct {
	ID        int    `json:"id" db:"id"`
	CompanyID int    `json:"company_id" db:"company_id"`
	Name      string `json:"name" db:"name"`
}
type MemberInfo struct {
	UserID      int    `json:"user_id" db:"user_id"`
	CompanyID   int    `json:"company_id" db:"company_id"`
	Role        string `json:"role" db:"role"`
	CustomTitle string `json:"custom_title" db:"custom_title"` // ВАЖНО: поле должно быть тут
	CompanyName string `json:"company_name" db:"company_name"`
	UserName    string `json:"user_name" db:"user_name"`
}
type ProcurementItem struct {
	PositionName string  `json:"position_name" db:"position_name"`
	Quantity     float64 `json:"quantity" db:"quantity"`
	Unit         string  `json:"unit" db:"unit"`
	IsUnlisted   bool    `json:"is_unlisted" db:"is_unlisted"`
	Supplier     string  `json:"supplier"` // Добавим для фронтенда
}

type ProcurementRequest struct {
	ID        int               `json:"id" db:"id"`
	CompanyID int               `json:"company_id" db:"company_id"`
	UserID    int               `json:"user_id" db:"user_id"`
	UserName  string            `json:"user_name" db:"user_name"` // Имя того, кто создал
	Status    string            `json:"status" db:"status"`       // pending, approved, rejected
	CreatedAt time.Time         `json:"created_at" db:"created_at"`
	Items     []ProcurementItem `json:"items"`                    // Список товаров в заявке
}
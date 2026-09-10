package service

import (
	"fmt"
	"github.com/jmoiron/sqlx"
	"qa2a/internal/repository"
	"strings"
	"time"
)

type MarketplaceService struct {
	repo *repository.Repository
}

func NewMarketplaceService(r *repository.Repository) *MarketplaceService {
	return &MarketplaceService{repo: r}
}

type OfferResponse struct {
	ID         int     `json:"id" db:"id"`
	SupplierID int     `json:"supplier_id,omitempty" db:"supplier_id"`
	Title      string  `json:"title" db:"title"`
	Desc       string  `json:"description" db:"description"`
	PriceType  string  `json:"price_type,omitempty" db:"price_type"`
	PriceValue float64 `json:"price_value,omitempty" db:"price_value"`
	PriceStr   string  `json:"priceStr" db:"-"`
	Keywords   string  `json:"keywords,omitempty" db:"keywords"`
    ViewsCount  int    `json:"views_count" db:"views_count"`
    ClicksCount int    `json:"clicks_count" db:"clicks_count"`
}

// GetOffersForCompany gets targeted offers for a restaurant
func (s *MarketplaceService) GetOffersForCompany(companyID int) ([]OfferResponse, error) {
	db := s.repo.GetDb()

	type OfferRow struct {
		ID         int     `db:"id"`
		Title      string  `db:"title"`
		Desc       string  `db:"description"`
		PriceType  string  `db:"price_type"`
		PriceValue float64 `db:"price_value"`
		Keywords   string  `db:"keywords"`
	}

	var activeOffers []OfferRow
	err := db.Select(&activeOffers, "SELECT id, title, description, price_type, price_value, keywords::text FROM marketplace_offers WHERE is_active = true")
	if err != nil {
		return nil, fmt.Errorf("failed to fetch offers: %w", err)
	}

	var purchasedCategories []string
	_ = db.Select(&purchasedCategories, "SELECT DISTINCT clean_category FROM purchase_history WHERE company_id = $1 AND clean_category != '", companyID)

	var result []OfferResponse
	for _, o := range activeOffers {
		priceStr := ""
		if o.PriceType == "exact" {
			priceStr = fmt.Sprintf("%.2f ₽", o.PriceValue)
		} else if o.PriceType == "from" {
			priceStr = fmt.Sprintf("от %.2f ₽", o.PriceValue)
		} else {
			priceStr = "По запросу"
		}

		matched := false
		if len(purchasedCategories) == 0 {
			matched = true
		} else {
			kwLower := strings.ToLower(o.Keywords)
			for _, cat := range purchasedCategories {
				if cat != "" && strings.Contains(kwLower, strings.ToLower(cat)) {
					matched = true
					break
				}
			}
			if !matched && len(kwLower) <= 4 {
				matched = true
			}
		}

		if matched || len(result) < 2 {
			result = append(result, OfferResponse{
				ID:       o.ID,
				Title:    o.Title,
				Desc:     o.Desc,
				PriceStr: priceStr,
			})
		}
	}
	return result, nil
}

// ---------------- SUPPLIER PORTAL METHODS ----------------

func (s *MarketplaceService) GetSupplierIDByTgID(tgID int64) (int, error) {
	var supplierID int
	err := s.repo.GetDb().Get(&supplierID, "SELECT supplier_id FROM marketplace_supplier_users WHERE tg_id = $1 AND is_active = true LIMIT 1", tgID)
	if err != nil {
		return 0, err
	}
	return supplierID, nil
}

func (s *MarketplaceService) GetSupplierOffers(supplierID int) ([]OfferResponse, error) {
	db := s.repo.GetDb()
	var offers []OfferResponse

	rows, err := db.Queryx("SELECT id, title, description, price_type, price_value, keywords::text, views_count, clicks_count FROM marketplace_offers WHERE supplier_id = $1 ORDER BY id DESC", supplierID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var o OfferResponse
		if err := rows.StructScan(&o); err != nil {
			continue
		}
		if o.PriceType == "exact" {
			o.PriceStr = fmt.Sprintf("%.2f ₽", o.PriceValue)
		} else if o.PriceType == "from" {
			o.PriceStr = fmt.Sprintf("от %.2f ₽", o.PriceValue)
		} else {
			o.PriceStr = "По запросу"
		}
		offers = append(offers, o)
	}
	return offers, nil
}

type CreateOfferReq struct {
    ID         int     `json:"id"`
	Title      string  `json:"title"`
	Desc       string  `json:"desc"`
	PriceType  string  `json:"price_type"`
	PriceValue float64 `json:"price_value"`
	Keywords   string  `json:"keywords"`
}

func (s *MarketplaceService) SaveSupplierOffer(supplierID int, req CreateOfferReq) error {
	db := s.repo.GetDb()
    if req.ID > 0 {
		_, err := db.Exec(`
			UPDATE marketplace_offers 
			SET title = $1, description = $2, price_type = $3, price_value = $4, keywords = $5::jsonb
			WHERE id = $6 AND supplier_id = $7
		`, req.Title, req.Desc, req.PriceType, req.PriceValue, req.Keywords, req.ID, supplierID)
		return err
	}
	_, err := db.Exec(`
        INSERT INTO marketplace_offers (supplier_id, title, description, price_type, price_value, keywords)
        VALUES ($1, $2, $3, $4, $5, $6::jsonb)
    `, supplierID, req.Title, req.Desc, req.PriceType, req.PriceValue, req.Keywords)
	return err
}

func (s *MarketplaceService) DeleteSupplierOffer(supplierID, offerID int) error {
	_, err := s.repo.GetDb().Exec("DELETE FROM marketplace_offers WHERE id = $1 AND supplier_id = $2", offerID, supplierID)
	return err
}

func (s *MarketplaceService) RegisterSupplier(tgID int64, username string) (int, error) {
	db := s.repo.GetDb()
	var supplierID int
	err := db.Get(&supplierID, "SELECT id FROM marketplace_suppliers WHERE company_name = $1 LIMIT 1", username)
	if err != nil {
		// Generate an invite code
		inviteCode := fmt.Sprintf("SUP-%d", time.Now().UnixNano()%1000000)
		err = db.QueryRow("INSERT INTO marketplace_suppliers (company_name, invite_code) VALUES ($1, $2) RETURNING id", username, inviteCode).Scan(&supplierID)
		if err != nil {
			return 0, err
		}
	}
	_, err = db.Exec("INSERT INTO marketplace_supplier_users (supplier_id, tg_id, tg_username) VALUES ($1, $2, $3) ON CONFLICT (tg_id) DO UPDATE SET supplier_id = EXCLUDED.supplier_id, tg_username = EXCLUDED.tg_username", supplierID, tgID, username)
	return supplierID, err
}

func (s *MarketplaceService) RecordOfferViews(offerIDs []int) error {
	if len(offerIDs) == 0 {
		return nil
	}
	db := s.repo.GetDb()
	for _, id := range offerIDs {
		db.Exec("UPDATE marketplace_offers SET views_count = views_count + 1 WHERE id = $1", id)
	}
	return nil
}

func (s *MarketplaceService) RecordOfferClick(offerID int) error {
	db := s.repo.GetDb()
	_, err := db.Exec("UPDATE marketplace_offers SET clicks_count = clicks_count + 1 WHERE id = $1", offerID)
	return err
}


func (s *MarketplaceService) JoinSupplier(tgID int64, code string) error {
	db := s.repo.GetDb()
	var supplierID int
	err := db.Get(&supplierID, "SELECT id FROM marketplace_suppliers WHERE invite_code = $1", code)
	if err != nil {
		return fmt.Errorf("Неверный код поставщика")
	}
	_, err = db.Exec("INSERT INTO marketplace_supplier_users (supplier_id, tg_id, tg_username) VALUES ($1, $2, $3) ON CONFLICT (tg_id) DO UPDATE SET supplier_id = EXCLUDED.supplier_id", supplierID, tgID, "user")
	return err
}


func (s *MarketplaceService) GetDb() *sqlx.DB { return s.repo.GetDb() }

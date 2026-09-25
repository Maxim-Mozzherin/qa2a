package main

import (
	"database/sql"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPriceSpikeSentinelCondition(t *testing.T) {
	checkSpike := func(median sql.NullFloat64, pricePerUnit float64, itemSum float64) bool {
		if !median.Valid || median.Float64 <= 0 {
			return false
		}
		if itemSum <= 500 {
			return false
		}
		return pricePerUnit > median.Float64*1.15
	}

	// 1. Normal purchase without spike (10%)
	if checkSpike(sql.NullFloat64{Float64: 100.0, Valid: true}, 110.0, 1000.0) {
		t.Error("expected no spike for 10% increase, got spike")
	}

	// 2. Normal purchase (12%)
	if checkSpike(sql.NullFloat64{Float64: 100.0, Valid: true}, 112.0, 1000.0) {
		t.Error("expected no spike for 12% increase, got spike")
	}

	// 3. True spike: 100 -> 120 (>15%) and sum > 500
	if !checkSpike(sql.NullFloat64{Float64: 100.0, Valid: true}, 120.0, 1200.0) {
		t.Error("expected spike for 20% increase and sum 1200, got false")
	}

	// 4. Micro-purchase: 100 -> 200 (100% increase) but sum <= 500 (e.g. 300 RUB)
	if checkSpike(sql.NullFloat64{Float64: 100.0, Valid: true}, 200.0, 300.0) {
		t.Error("expected micro-purchase (sum <= 500) to be ignored, got spike")
	}

	// 5. No history (NULL median)
	if checkSpike(sql.NullFloat64{Valid: false}, 200.0, 5000.0) {
		t.Error("expected NULL median to be ignored safely without panic, got spike")
	}

	// 6. Zero median
	if checkSpike(sql.NullFloat64{Float64: 0.0, Valid: true}, 200.0, 5000.0) {
		t.Error("expected zero median to be ignored safely without panic, got spike")
	}
}

func TestReconciliationMatchingAlgorithm(t *testing.T) {
	type ActItem struct {
		DocNumber string
		DocDate   string
		Amount    float64
	}
	type DbInv struct {
		InvoiceNumber string
		DocDate       string
		TotalSum      float64
	}

	actItems := []ActItem{
		{DocNumber: "УПД-101", DocDate: "2026-03-01", Amount: 10000.00}, // Matched (exact)
		{DocNumber: "УПД-102", DocDate: "2026-03-05", Amount: 5001.50},  // Matched (delta 1.50 <= 2.00)
		{DocNumber: "УПД-103", DocDate: "2026-03-10", Amount: 8000.00},  // Mismatched (delta 500.00 > 2.00)
		{DocNumber: "УПД-104", DocDate: "2026-03-15", Amount: 12500.00}, // Missing in DB
	}

	dbInvoices := map[string]DbInv{
		"УПД-101": {InvoiceNumber: "УПД-101", DocDate: "2026-03-01", TotalSum: 10000.00},
		"УПД-102": {InvoiceNumber: "УПД-102", DocDate: "2026-03-05", TotalSum: 5000.00},
		"УПД-103": {InvoiceNumber: "УПД-103", DocDate: "2026-03-10", TotalSum: 7500.00},
		"УПД-999": {InvoiceNumber: "УПД-999", DocDate: "2026-03-12", TotalSum: 3000.00}, // Phantom in DB
	}

	matchedDbInvoices := make(map[string]bool)
	var matched, mismatched, missingInDb, phantomInDb []ReconciliationMatchItem

	for _, act := range actItems {
		dbInv, found := dbInvoices[act.DocNumber]
		if !found {
			for k, v := range dbInvoices {
				if strings.EqualFold(k, act.DocNumber) {
					dbInv = v
					found = true
					break
				}
			}
		}

		if !found {
			missingInDb = append(missingInDb, ReconciliationMatchItem{
				DocNumber: act.DocNumber,
				DocDate:   act.DocDate,
				ActAmount: act.Amount,
				Status:    "missing_in_db",
			})
		} else {
			matchedDbInvoices[dbInv.InvoiceNumber] = true
			diff := math.Abs(act.Amount - dbInv.TotalSum)
			if diff <= 2.00 {
				matched = append(matched, ReconciliationMatchItem{
					DocNumber: act.DocNumber,
					DocDate:   act.DocDate,
					ActAmount: act.Amount,
					DbAmount:  dbInv.TotalSum,
					Diff:      diff,
					Status:    "matched",
				})
			} else {
				mismatched = append(mismatched, ReconciliationMatchItem{
					DocNumber: act.DocNumber,
					DocDate:   act.DocDate,
					ActAmount: act.Amount,
					DbAmount:  dbInv.TotalSum,
					Diff:      act.Amount - dbInv.TotalSum,
					Status:    "mismatched",
				})
			}
		}
	}

	for k, dbInv := range dbInvoices {
		if !matchedDbInvoices[k] {
			phantomInDb = append(phantomInDb, ReconciliationMatchItem{
				DocNumber: dbInv.InvoiceNumber,
				DocDate:   dbInv.DocDate,
				DbAmount:  dbInv.TotalSum,
				Status:    "phantom_in_db",
			})
		}
	}

	if len(matched) != 2 {
		t.Fatalf("expected 2 matched items, got %d", len(matched))
	}
	if len(mismatched) != 1 {
		t.Fatalf("expected 1 mismatched item, got %d", len(mismatched))
	}
	if len(missingInDb) != 1 {
		t.Fatalf("expected 1 missing item, got %d", len(missingInDb))
	}
	if len(phantomInDb) != 1 {
		t.Fatalf("expected 1 phantom item, got %d", len(phantomInDb))
	}

	if matched[0].DocNumber != "УПД-101" || matched[1].DocNumber != "УПД-102" {
		t.Errorf("unexpected matched items: %+v", matched)
	}
	if mismatched[0].DocNumber != "УПД-103" || mismatched[0].Diff != 500.00 {
		t.Errorf("unexpected mismatched item: %+v", mismatched[0])
	}
	if missingInDb[0].DocNumber != "УПД-104" {
		t.Errorf("unexpected missing item: %+v", missingInDb[0])
	}
	if phantomInDb[0].DocNumber != "УПД-999" {
		t.Errorf("unexpected phantom item: %+v", phantomInDb[0])
	}
}

func TestHandleGetReconciliationRegistry_Validation(t *testing.T) {
	// 1. Invalid company ID -> 400
	req1 := httptest.NewRequest("GET", "/api/reconciliation/registry?company_id=abc", nil)
	rec1 := httptest.NewRecorder()
	handleGetReconciliationRegistry(rec1, req1)
	if rec1.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec1.Code)
	}

	// 2. Unauthorized user -> 403
	req2 := httptest.NewRequest("GET", "/api/reconciliation/registry?company_id=999&date_from=2026-01-01&date_to=2026-01-31", nil)
	user := &AuthUser{ID: 10, Login: "acc", Role: "accountant", AccountingFirmID: nil}
	reqWithCtx := req2.WithContext(ContextWithAuthUser(req2.Context(), user))
	rec2 := httptest.NewRecorder()
	handleGetReconciliationRegistry(rec2, reqWithCtx)
	if rec2.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", rec2.Code)
	}

	// 3. Superadmin without dates -> 400
	req3 := httptest.NewRequest("GET", "/api/reconciliation/registry?company_id=1", nil)
	super := &AuthUser{ID: 1, Login: "root", Role: "superadmin"}
	req3WithCtx := req3.WithContext(ContextWithAuthUser(req3.Context(), super))
	rec3 := httptest.NewRecorder()
	handleGetReconciliationRegistry(rec3, req3WithCtx)
	if rec3.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec3.Code)
	}

	// 4. Invalid date format -> 400
	req4 := httptest.NewRequest("GET", "/api/reconciliation/registry?company_id=1&date_from=2026/01/01&date_to=2026-01-31", nil)
	req4WithCtx := req4.WithContext(ContextWithAuthUser(req4.Context(), super))
	rec4 := httptest.NewRecorder()
	handleGetReconciliationRegistry(rec4, req4WithCtx)
	if rec4.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid date format, got %d", rec4.Code)
	}
}

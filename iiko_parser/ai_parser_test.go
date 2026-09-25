package main

import (
	"testing"
)

func TestIsSubtotalRow(t *testing.T) {
	subtotals := []string{
		"Итого по странице",
		"Всего по странице: 14200.50",
		"Промежуточный итог",
		"Всего перенесено",
		"Страница 2 из 5",
		"Лист 3",
		"",
	}
	for _, s := range subtotals {
		if !isSubtotalRow(s) {
			t.Errorf("Expected isSubtotalRow(%q) to be true, got false", s)
		}
	}

	validItems := []string{
		"Томаты черри 500г",
		"Сливки 33% 1л",
		"Масло сливочное 82.5% 180г",
		"Кофе зерновой 1кг",
	}
	for _, it := range validItems {
		if isSubtotalRow(it) {
			t.Errorf("Expected isSubtotalRow(%q) to be false, got true", it)
		}
	}
}

func TestIsDuplicateJunction(t *testing.T) {
	prev := AiItem{
		Name:     "Сливки 33% Петмол 1л",
		Quantity: 12.0,
		Price:    420.50,
	}
	// Exact duplicate on next page junction
	dup := AiItem{
		Name:     "Сливки 33% Петмол 1л",
		Quantity: 12.0,
		Price:    420.50,
	}
	if !isDuplicateJunction(prev, dup) {
		t.Errorf("Expected isDuplicateJunction to be true for identical junction item")
	}

	// Different name
	diffName := AiItem{
		Name:     "Молоко 3.2% 1л",
		Quantity: 12.0,
		Price:    420.50,
	}
	if isDuplicateJunction(prev, diffName) {
		t.Errorf("Expected isDuplicateJunction to be false for different name")
	}

	// Different quantity
	diffQty := AiItem{
		Name:     "Сливки 33% Петмол 1л",
		Quantity: 6.0,
		Price:    420.50,
	}
	if isDuplicateJunction(prev, diffQty) {
		t.Errorf("Expected isDuplicateJunction to be false for different quantity")
	}
}

func TestMergePageResponses(t *testing.T) {
	page1 := &AiResponse{
		VendorName:   "ООО Фреш Маркет",
		VendorINN:    "7701234567",
		DocNumber:    "УПД-9988",
		DocDate:      "2026-09-25",
		Consignee:    "Кафе Веранда",
		ConsigneeINN: "7709876543",
		Shipper:      "ООО Фреш Маркет Склад 1",
		UsedModel:    "gemini/gemini-2.0-flash",
		Items: []AiItem{
			{Name: "Томаты розовые 1кг", Quantity: 10, Price: 250, Sum: 2500},
			{Name: "Огурцы короткоплодные", Quantity: 5, Price: 180, Sum: 900},
			{Name: "Сливки 33% 1л", Quantity: 12, Price: 400, Sum: 4800},
		},
	}

	page2 := &AiResponse{
		DocPrintedTotalSum: 12500.00,
		Items: []AiItem{
			// Duplicate from boundary of page 1
			{Name: "Сливки 33% 1л", Quantity: 12, Price: 400, Sum: 4800},
			// Subtotal line from invoice
			{Name: "Итого по странице 2", Quantity: 0, Price: 0, Sum: 4800},
			// New valid items on page 2
			{Name: "Сыр Моцарелла 1кг", Quantity: 4, Price: 800, Sum: 3200},
			{Name: "Зелень Руккола 125г", Quantity: 10, Price: 110, Sum: 1100},
		},
	}

	merged := mergePageResponses([]*AiResponse{page1, page2})

	if merged.VendorName != "ООО Фреш Маркет" {
		t.Errorf("Expected VendorName to be preserved from page 1, got %q", merged.VendorName)
	}
	if merged.DocNumber != "УПД-9988" {
		t.Errorf("Expected DocNumber to be preserved from page 1, got %q", merged.DocNumber)
	}
	if merged.DocPrintedTotalSum != 12500.00 {
		t.Errorf("Expected DocPrintedTotalSum to be 12500.00 from page 2, got %v", merged.DocPrintedTotalSum)
	}

	// Total expected items: 3 (from page 1) + 2 (from page 2, after 1 duplicate and 1 subtotal removed) = 5
	expectedCount := 5
	if len(merged.Items) != expectedCount {
		t.Fatalf("Expected %d items after merge, got %d", expectedCount, len(merged.Items))
	}

	// Verify sequential numbering 1..5
	for i, item := range merged.Items {
		if item.Num != i+1 {
			t.Errorf("Item %d has Num=%d, expected %d", i, item.Num, i+1)
		}
	}

	// Check items sequence
	if merged.Items[0].Name != "Томаты розовые 1кг" ||
		merged.Items[1].Name != "Огурцы короткоплодные" ||
		merged.Items[2].Name != "Сливки 33% 1л" ||
		merged.Items[3].Name != "Сыр Моцарелла 1кг" ||
		merged.Items[4].Name != "Зелень Руккола 125г" {
		t.Errorf("Unexpected items sequence after merge: %+v", merged.Items)
	}
}

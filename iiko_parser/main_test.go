package main

import (
	"testing"
)

func TestNormalizeVendorName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{`ООО "РЕМО"`, "ремо"},
		{`Общество с ограниченной ответственностью "РЕМО"`, "ремо"},
		{`ООО «РЕМО» ИНН 7701234567 КПП 770101001`, "ремо"},
		{`ИП Иванов Иван Иванович`, "иванов иван иванович"},
		{`  ЗАО   "Мясной Дом"  `, "мясной дом"},
		{`АО "ТД ЭФКО"`, "тд эфко"},
	}

	for _, tt := range tests {
		got := normalizeVendorName(tt.input)
		if got != tt.expected {
			t.Errorf("normalizeVendorName(%q) = %q; want %q", tt.input, got, tt.expected)
		}
	}
}

func TestNormalizeItemName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{`Масло сладко-сливочное 82,5%`, "масло сладко сливочное 82 5%"},
		{`Сыр "Моцарелла" 45%`, "сыр моцарелла 45%"},
		{`Томаты в собственном соку`, "томаты в собственном соку"}, // non-breaking spaces \u00a0
		{`Шампиньоны свежие (1 сорт)`, "шампиньоны свежие 1 сорт"},
		{`Лосось с/м б/к`, "лосось с м б к"},
	}

	for _, tt := range tests {
		got := normalizeItemName(tt.input)
		if got != tt.expected {
			t.Errorf("normalizeItemName(%q) = %q; want %q", tt.input, got, tt.expected)
		}
	}
}

type testCandidate struct {
	VendorName   string
	VendorItem   string
	InternalUUID string
	InternalName string
	Multiplier   float64
}

func matchItem(vendorCandidates []testCandidate, allCompanyCandidates []testCandidate, itemName string) *testCandidate {
	itemNorm := normalizeItemName(itemName)

	// Уровень 1: Точное совпадение наименования у данного поставщика
	for _, cm := range vendorCandidates {
		if cm.VendorItem == itemName {
			return &cm
		}
	}

	// Уровень 2: Нормализованное совпадение
	if itemNorm != "" {
		for _, cm := range vendorCandidates {
			if normalizeItemName(cm.VendorItem) == itemNorm {
				return &cm
			}
		}
	}

	// Уровень 3: Префиксное / подстрочное совпадение
	if len(itemNorm) >= 4 {
		for _, cm := range vendorCandidates {
			cNorm := normalizeItemName(cm.VendorItem)
			if len(cNorm) >= 4 && (len(itemNorm) >= len(cNorm) && itemNorm[:len(cNorm)] == cNorm || len(cNorm) >= len(itemNorm) && cNorm[:len(itemNorm)] == itemNorm) {
				return &cm
			}
		}
	}

	// Уровень 4: Точное или нормализованное совпадение по другим поставщикам заведения
	for _, cm := range allCompanyCandidates {
		if cm.VendorItem == itemName || (itemNorm != "" && normalizeItemName(cm.VendorItem) == itemNorm) {
			return &cm
		}
	}

	return nil
}

func TestMatchingCandidate(t *testing.T) {
	vendorList := []testCandidate{
		{VendorName: "ООО РЕМО", VendorItem: "Масло сливочное 82,5%", InternalUUID: "uuid-1", InternalName: "Масло 82.5%", Multiplier: 1.0},
		{VendorName: "ООО РЕМО", VendorItem: "Сыр Моцарелла 45%", InternalUUID: "uuid-2", InternalName: "Сыр Моцарелла Pizza", Multiplier: 2.0},
	}
	allList := []testCandidate{
		{VendorName: "ООО Другой", VendorItem: "Сахар-песок 1кг", InternalUUID: "uuid-3", InternalName: "Сахар", Multiplier: 1.0},
	}

	// 1. Exact match
	m1 := matchItem(vendorList, allList, "Масло сливочное 82,5%")
	if m1 == nil || m1.InternalUUID != "uuid-1" {
		t.Fatalf("Expected uuid-1, got %v", m1)
	}

	// 2. Normalized match with quotes and punctuation
	m2 := matchItem(vendorList, allList, `Сыр "Моцарелла" 45%`)
	if m2 == nil || m2.InternalUUID != "uuid-2" {
		t.Fatalf("Expected uuid-2, got %v", m2)
	}

	// 3. Fallback across company
	m3 := matchItem(vendorList, allList, "Сахар песок 1кг")
	if m3 == nil || m3.InternalUUID != "uuid-3" {
		t.Fatalf("Expected uuid-3, got %v", m3)
	}
}


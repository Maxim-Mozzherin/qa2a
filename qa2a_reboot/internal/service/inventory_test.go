package service

import (
	"testing"
	"time"
)

func TestWriteOff_QuantityValidation(t *testing.T) {
	svc := &InventoryService{}

	// Test negative quantity
	err := svc.WriteOff(1, 1, "Тестовый товар", -5, "кг", 1, false, "списание", "", time.Now())
	if err == nil {
		t.Fatal("expected error for negative quantity, got nil")
	}
	expected := "количество товара для списания должно быть строго больше нуля"
	if err.Error() != expected {
		t.Fatalf("expected error message %q, got %q", expected, err.Error())
	}

	// Test zero quantity
	err = svc.WriteOff(1, 1, "Тестовый товар", 0, "кг", 1, false, "списание", "", time.Now())
	if err == nil {
		t.Fatal("expected error for zero quantity, got nil")
	}
	if err.Error() != expected {
		t.Fatalf("expected error message %q, got %q", expected, err.Error())
	}
}

func TestSetInitialBalance_Validation(t *testing.T) {
	svc := &InventoryService{}

	// Test negative quantity
	err := svc.SetInitialBalance(1, 1, "Тестовый товар", -10, "кг", 1)
	if err == nil {
		t.Fatal("expected error for negative quantity in SetInitialBalance, got nil")
	}

	// Test zero quantity
	err = svc.SetInitialBalance(1, 1, "Тестовый товар", 0, "кг", 1)
	if err == nil {
		t.Fatal("expected error for zero quantity in SetInitialBalance, got nil")
	}

	// Test invalid location
	err = svc.SetInitialBalance(1, 1, "Тестовый товар", 10, "кг", 0)
	if err == nil {
		t.Fatal("expected error for invalid locationID in SetInitialBalance, got nil")
	}
}

func TestTransfer_LocationValidation(t *testing.T) {
	svc := &InventoryService{}

	// Same from and to location
	err := svc.Transfer(1, 1, "Сливки", 5, "л", 2, 2, "перемещение", time.Now())
	if err == nil {
		t.Fatal("expected error when fromLoc == toLoc")
	}

	// Invalid from location
	err = svc.Transfer(1, 1, "Сливки", 5, "л", 0, 2, "перемещение", time.Now())
	if err == nil {
		t.Fatal("expected error when fromLoc <= 0")
	}
}

func TestEscapeXML_Security(t *testing.T) {
	cases := []struct {
		input    string
		expected string
	}{
		{"Мясо & Овощи", "Мясо &amp; Овощи"},
		{"<script>alert(1)</script>", "&lt;script&gt;alert(1)&lt;/script&gt;"},
		{`"ООО" 'Вкус'`, "&#34;ООО&#34; &#39;Вкус&#39;"},
		{"Чистый текст 123", "Чистый текст 123"},
	}

	for _, c := range cases {
		got := escapeXML(c.input)
		if got != c.expected {
			t.Errorf("escapeXML(%q) = %q; expected %q", c.input, got, c.expected)
		}
	}
}

func TestTransfer_PositionValidation(t *testing.T) {
	svc := &InventoryService{}

	// Empty position name
	err := svc.Transfer(1, 1, "   ", 5, "л", 1, 2, "перемещение", time.Now())
	if err == nil {
		t.Fatal("expected error for empty posName in Transfer")
	}
}

func TestMarketplace_OfferValidation(t *testing.T) {
	mkt := &MarketplaceService{}

	// Empty title
	err := mkt.SaveSupplierOffer(1, CreateOfferReq{
		Title: "   ",
	})
	if err == nil {
		t.Fatal("expected error for empty offer title")
	}

	// Negative price
	err = mkt.SaveSupplierOffer(1, CreateOfferReq{
		Title:      "Сыр Моцарелла",
		PriceValue: -100,
	})
	if err == nil {
		t.Fatal("expected error for negative price")
	}
}

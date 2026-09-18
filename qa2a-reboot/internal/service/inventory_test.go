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

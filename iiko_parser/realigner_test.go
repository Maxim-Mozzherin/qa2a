package main

import (
	"math"
	"testing"
)

func TestRealigner_OffByOneShiftCorrection(t *testing.T) {
	// Симуляция данных, которые нейросеть извлекла с ошибкой сдвига на 1 строку:
	// Капуста получила вес Картофеля (4.08), Картофель получил вес Кинзы (1.1),
	// Кинза получила вес Клубники (2.0), а последняя позиция Шимиджи захватила итог 25.4.
	// Цены и суммы на строках сохранились правильные из колонок 11 и 15.
	rawResponse := &AiResponse{
		DocPrintedTotalSum: 4481.20,
		Items: []AiItem{
			{Name: "Капуста б/к", Quantity: 4.08, Price: 130.00, Sum: 390.00, Unit: "кг", BaseUnit: "кг", AiMultiplier: 1.0},
			{Name: "Картофель", Quantity: 1.10, Price: 40.00, Sum: 163.20, Unit: "кг", BaseUnit: "кг", AiMultiplier: 1.0},
			{Name: "Кинза", Quantity: 2.00, Price: 580.00, Sum: 638.00, Unit: "кг", BaseUnit: "кг", AiMultiplier: 1.0},
			{Name: "Клубника", Quantity: 2.00, Price: 1550.00, Sum: 3100.00, Unit: "кг", BaseUnit: "кг", AiMultiplier: 1.0},
			{Name: "Грибы Шимиджи", Quantity: 25.40, Price: 195.00, Sum: 1560.00, Unit: "шт", BaseUnit: "шт", AiMultiplier: 0.15},
		},
	}

	realigned := realignAndValidateInvoice(rawResponse)

	// 1. Капуста должна получить свое истинное количество 3.000 кг (390 / 130)
	if math.Abs(realigned.Items[0].Quantity-3.000) > 0.001 {
		t.Errorf("Expected Cabbage Quantity to be 3.000, got %.3f", realigned.Items[0].Quantity)
	}
	if realigned.Items[0].Price != 130.00 {
		t.Errorf("Expected Cabbage Price to remain 130.00, got %.2f", realigned.Items[0].Price)
	}

	// 2. Картофель должен получить свое истинное количество 4.080 кг (163.20 / 40)
	if math.Abs(realigned.Items[1].Quantity-4.080) > 0.001 {
		t.Errorf("Expected Potato Quantity to be 4.080, got %.3f", realigned.Items[1].Quantity)
	}
	if realigned.Items[1].Price != 40.00 {
		t.Errorf("Expected Potato Price to remain 40.00, got %.2f", realigned.Items[1].Price)
	}

	// 3. Кинза должна получить свое истинное количество 1.100 кг (638 / 580)
	if math.Abs(realigned.Items[2].Quantity-1.100) > 0.001 {
		t.Errorf("Expected Cilantro Quantity to be 1.100, got %.3f", realigned.Items[2].Quantity)
	}

	// 4. Грибы Шимиджи должны получить 8.000 шт (1560 / 195), а не захваченный итог 25.40
	if math.Abs(realigned.Items[4].Quantity-8.000) > 0.001 {
		t.Errorf("Expected Shimiji Quantity to be 8.000, got %.3f", realigned.Items[4].Quantity)
	}
	if realigned.Items[4].Price != 195.00 {
		t.Errorf("Expected Shimiji Price to remain 195.00, got %.2f", realigned.Items[4].Price)
	}
}

func TestRealigner_UpdPriceWithoutVat(t *testing.T) {
	// УПД: цена без НДС (100.00), ставка 20%, сумма с НДС 240.00, кол-во 2.0
	raw := &AiResponse{
		Items: []AiItem{
			{Name: "Сыр", Quantity: 2.0, Price: 100.00, Sum: 240.00, SumWithoutNds: 200.00, NdsPercent: 20.0, Unit: "кг"},
		},
	}
	realigned := realignAndValidateInvoice(raw)

	// Цена должна быть нормализована до цены С НДС = 120.00
	if math.Abs(realigned.Items[0].Price-120.00) > 0.01 {
		t.Errorf("Expected Cheese Price with VAT to be 120.00, got %.2f", realigned.Items[0].Price)
	}
	if math.Abs(realigned.Items[0].Quantity-2.0) > 0.001 {
		t.Errorf("Expected Cheese Quantity to remain 2.0, got %.3f", realigned.Items[0].Quantity)
	}
}

func TestRealigner_BlueberryFormats(t *testing.T) {
	raw := &AiResponse{
		Items: []AiItem{
			{Name: "Голубика свежая", Quantity: 5.0, Price: 250.00, Sum: 1250.00, Unit: "шт", AiMultiplier: 1.0},
			{Name: "Голубика весовая", Quantity: 2.5, Price: 1800.00, Sum: 4500.00, Unit: "кг", AiMultiplier: 1.0},
		},
	}
	realigned := realignAndValidateInvoice(raw)

	// 1. Голубика в шт должна получить фасовку 0.125
	if realigned.Items[0].AiMultiplier != 0.125 {
		t.Errorf("Expected Blueberry in pcs to have multiplier 0.125, got %.3f", realigned.Items[0].AiMultiplier)
	}
	if realigned.Items[0].BaseUnit != "кг" {
		t.Errorf("Expected Blueberry in pcs to have base_unit 'кг', got %s", realigned.Items[0].BaseUnit)
	}

	// 2. Голубика в кг должна остаться с фасовкой 1.0
	if realigned.Items[1].AiMultiplier != 1.0 {
		t.Errorf("Expected Blueberry in kg to have multiplier 1.0, got %.3f", realigned.Items[1].AiMultiplier)
	}
}

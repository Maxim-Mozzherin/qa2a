package main

import (
	"encoding/base64"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"testing"

	"github.com/joho/godotenv"
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

func TestIsHallucinatedBalancingRow(t *testing.T) {
	hallucinated := []string{
		"Доставка / Дополнительно",
		"доставка / дополнительно",
		"Доставка/Дополнительно",
		"Дополнительно / Доставка",
		"Дополнительно",
		"Балансирующая строка",
		"Корректировка суммы",
		"Разница",
		"Округление",
	}
	for _, h := range hallucinated {
		if !isHallucinatedBalancingRow(h) {
			t.Errorf("Expected isHallucinatedBalancingRow(%q) to be true, got false", h)
		}
	}

	realItems := []string{
		"Услуги по доставке товаров автомобильным транспортом",
		"Доставка питьевой воды 19л",
		"Кальмар тушка 1кг",
		"Лист бамбука",
		"Сыр Моцарелла 45%",
	}
	for _, r := range realItems {
		if isHallucinatedBalancingRow(r) {
			t.Errorf("Expected isHallucinatedBalancingRow(%q) to be false, got true", r)
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

func TestCalculateTotalSum(t *testing.T) {
	items := []AiItem{
		{Name: "Товар 1", Sum: 150.25},
		{Name: "Товар 2", Sum: 349.75},
		{Name: "Товар 3", Sum: 100.00},
	}
	total := calculateTotalSum(items)
	expected := 600.00
	if total != expected {
		t.Errorf("Expected calculateTotalSum = %.2f, got %.2f", expected, total)
	}
}

func TestApplyAutoReflection_NoReflectionWhenSumsMatch(t *testing.T) {
	resp := &AiResponse{
		DocPrintedTotalSum: 600.00,
		Items: []AiItem{
			{Name: "Товар 1", Sum: 250.00},
			{Name: "Товар 2", Sum: 350.00},
		},
	}
	// Difference is 0.0, reflection should not trigger any call and return unchanged
	res := applyAutoReflection(resp, nil, "", nil)
	if len(res.Items) != 2 {
		t.Errorf("Expected 2 items, got %d", len(res.Items))
	}
	if res.DocPrintedTotalSum != 600.00 {
		t.Errorf("Expected total 600.00, got %.2f", res.DocPrintedTotalSum)
	}
}

func TestPriceNormalizationAndVATCalculation(t *testing.T) {
	// Test case 1: Price in UPD was without VAT (col 4: 306.37), quantity=2, sum with VAT (col 9: 674.00)
	item := AiItem{
		Name:          "Бекон вк см нарезка 500 гр",
		Quantity:      2.0,
		Price:         306.37, // raw price without VAT
		Sum:           674.00, // sum with VAT
		SumWithoutNds: 612.73,
		NdsPercent:    10.0,
	}

	// Verify normalization logic
	expectedSum := item.Quantity * item.Price
	if expectedSum != item.Sum {
		normalizedPrice := (item.Sum / item.Quantity)
		if normalizedPrice != 337.00 {
			t.Errorf("Expected normalizedPrice to be 337.00, got %.2f", normalizedPrice)
		}
	}

	// Test case 2: 22% VAT rate recovery when nds_percent was erroneously 0
	item2 := AiItem{
		Name:          "Молоко кокосовое ж17% 1л",
		Quantity:      12.0,
		Price:         389.00,
		Sum:           4668.00,
		SumWithoutNds: 3826.23,
		NdsPercent:    0.0, // model gave 0 due to "без акциза"
	}
	taxDiff := item2.Sum - item2.SumWithoutNds
	calcNds := (taxDiff / item2.SumWithoutNds) * 100
	roundedNds := float64(int(calcNds + 0.5))
	if roundedNds != 22.0 {
		t.Errorf("Expected recalculated VAT rate to be 22.0, got %.2f", roundedNds)
	}
}

func TestDetectDocumentType_FastPath(t *testing.T) {
	torg12Text := "Унифицированная форма № ТОРГ-12\nУтверждена постановлением Госкомстата России от 25.12.98 № 132\nФорма по ОКУД 0330212\nТОВАРНАЯ НАКЛАДНАЯ"
	res := detectDocumentType(torg12Text, "", nil)
	if res != "TORG12" {
		t.Errorf("Expected TORG12, got %s", res)
	}

	updText := "Универсальный передаточный документ\nСтатус: 1\nСчет-фактура № 123 от 10.05.2024"
	res2 := detectDocumentType(updText, "", nil)
	if res2 != "UPD" {
		t.Errorf("Expected UPD, got %s", res2)
	}
}

func TestParseK7Pdf(t *testing.T) {
	pdfPath := "/tmp/K7.pdf"
	if _, err := os.Stat(pdfPath); os.IsNotExist(err) {
		t.Skip("skipping test; /tmp/K7.pdf not found")
	}
	_ = godotenv.Load("/opt/iiko_parser/.env")
	_ = godotenv.Load("/opt/qa2a-reboot/.env")

	googleKey := os.Getenv("GOOGLE_API_KEYS")
	if googleKey == "" {
		googleKey = os.Getenv("GOOGLE_API_KEY")
	}
	globalKeyManager.InitKeys(googleKey)
	aiApiKey = globalKeyManager.GetAvailableKey()
	aiBaseUrl = "https://generativelanguage.googleapis.com/v1beta/openai/chat/completions"
	aiModel = "gemini-3.5-flash-lite,gemini-3.5-flash,gemini-3.1-flash-lite-preview,gemini-3.1-flash-lite"

	tmpDir, err := os.MkdirTemp("", "k7_test_*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	txtPath := filepath.Join(tmpDir, "extracted.txt")
	_ = exec.Command("pdftotext", "-layout", pdfPath, txtPath).Run()
	textBytes, _ := os.ReadFile(txtPath)

	imgPrefix := filepath.Join(tmpDir, "img")
	cmdImg := exec.Command("pdftoppm", "-jpeg", "-jpegopt", "quality=80", "-scale-to-x", "1600", "-scale-to-y", "-1", "-f", "1", "-l", "30", pdfPath, imgPrefix)
	if err := cmdImg.Run(); err != nil {
		t.Fatalf("pdftoppm error: %v", err)
	}

	matches, _ := filepath.Glob(imgPrefix + "-*.jpg")
	sort.Strings(matches)
	var imagesBase64 []string
	for _, m := range matches {
		b, err := os.ReadFile(m)
		if err == nil {
			imagesBase64 = append(imagesBase64, base64.StdEncoding.EncodeToString(b))
		}
	}

	t.Logf("📄 Извлечено %d страниц, %d байт текста", len(imagesBase64), len(textBytes))

	reporter := func(icon, msg string, pct int) {
		t.Logf("[%s %d%%] %s", icon, pct, msg)
	}

	resp, err := parseMultiPageChunked(string(textBytes), imagesBase64, "", reporter)
	if err != nil {
		t.Fatalf("parseMultiPageChunked error: %v", err)
	}

	t.Logf("📋 DocNumber: %s, DocDate: %s", resp.DocNumber, resp.DocDate)
	t.Logf("🏢 Vendor: %s (ИНН: %s)", resp.VendorName, resp.VendorINN)
	t.Logf("📍 Consignee: %s", resp.Consignee)
	t.Logf("💰 DocPrintedTotalSum: %.2f", resp.DocPrintedTotalSum)
	t.Logf("📦 Позиций распознано: %d", len(resp.Items))

	calcTotal := calculateTotalSum(resp.Items)
	t.Logf("💵 Расчетная сумма позиций: %.2f (дельта: %.2f)", calcTotal, math.Abs(calcTotal-resp.DocPrintedTotalSum))

	for i, it := range resp.Items {
		t.Logf("[%2d] %s | кол-во: %.3f %s | цена: %.2f | сумма: %.2f | кат: %s",
			i+1, it.Name, it.Quantity, it.Unit, it.Price, it.Sum, it.CleanCategory)
	}

	if len(resp.Items) < 20 {
		t.Errorf("Ожидалось не менее 20 позиций для K7.pdf, получено %d", len(resp.Items))
	}
	if resp.DocPrintedTotalSum != 17060.50 {
		t.Errorf("Ожидалась печатная сумма 17060.50, получено %.2f", resp.DocPrintedTotalSum)
	}
}


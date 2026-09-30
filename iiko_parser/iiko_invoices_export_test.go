package main

import (
	"encoding/xml"
	"testing"
)

func TestIikoExportedInvoicesXMLParsing(t *testing.T) {
	xmlSample := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<incomingInvoiceDtoes>
	<document>
		<id>ec5170df-e0f6-6f12-01a0-4d7273bba01f</id>
		<transportInvoiceNumber>TR-999</transportInvoiceNumber>
		<incomingDocumentNumber>1765</incomingDocumentNumber>
		<incomingDate>2026-08-01</incomingDate>
		<useDefaultDocumentTime>false</useDefaultDocumentTime>
		<dueDate>2026-08-10</dueDate>
		<supplier>cfdd6790-d3bb-49a0-a55b-4178fa49fe8c</supplier>
		<defaultStore>aafe2a57-cd7c-4d5f-8efe-11c53db94d02</defaultStore>
		<invoice></invoice>
		<dateIncoming>2026-08-01T09:00:00</dateIncoming>
		<documentNumber>2452</documentNumber>
		<comment>Тестовая накладная</comment>
		<conception>dd9c5d7b-cf8d-4946-8cbb-28757c25d4c7</conception>
		<conceptionCode>03</conceptionCode>
		<status>PROCESSED</status>
		<distributionAlgorithm>DISTRIBUTION_BY_AMOUNT</distributionAlgorithm>
		<items>
			<item>
				<isAdditionalExpense>false</isAdditionalExpense>
				<actualAmount>20.000000000</actualAmount>
				<store>aafe2a57-cd7c-4d5f-8efe-11c53db94d02</store>
				<code>00598</code>
				<price>720.000000000</price>
				<priceWithoutVat>720.000000000</priceWithoutVat>
				<sum>14400.000000000</sum>
				<vatPercent>0.000000000</vatPercent>
				<vatSum>0.000000000</vatSum>
				<discountSum>0.000000000</discountSum>
				<amountUnit>7ba81c3a-8de5-8f9d-fb9f-e39efcbc57cc</amountUnit>
				<num>1</num>
				<product>89f9e106-2206-4456-bf54-1e9baf1b8a48</product>
				<productArticle>00598</productArticle>
				<amount>20.000000000</amount>
			</item>
			<item>
				<isAdditionalExpense>false</isAdditionalExpense>
				<actualAmount>10.500000000</actualAmount>
				<store>aafe2a57-cd7c-4d5f-8efe-11c53db94d02</store>
				<code>00600</code>
				<price>100.000000000</price>
				<priceWithoutVat>83.330000000</priceWithoutVat>
				<sum>1050.000000000</sum>
				<vatPercent>20.000000000</vatPercent>
				<vatSum>210.000000000</vatSum>
				<discountSum>0.000000000</discountSum>
				<amountUnit>7ba81c3a-8de5-8f9d-fb9f-e39efcbc57cc</amountUnit>
				<num>2</num>
				<product>aaaa1111-2222-3333-4444-555566667777</product>
				<productArticle>00600</productArticle>
				<amount>10.500000000</amount>
			</item>
		</items>
	</document>
</incomingInvoiceDtoes>`

	var parsed IikoExportedInvoicesXML
	if err := xml.Unmarshal([]byte(xmlSample), &parsed); err != nil {
		t.Fatalf("Ошибка парсинга XML: %v", err)
	}

	if len(parsed.Documents) != 1 {
		t.Fatalf("Ожидался 1 документ, получено %d", len(parsed.Documents))
	}

	doc := parsed.Documents[0]
	if doc.ID != "ec5170df-e0f6-6f12-01a0-4d7273bba01f" {
		t.Errorf("Неверный ID документа: %s", doc.ID)
	}
	if doc.IncomingDocumentNumber != "1765" {
		t.Errorf("Неверный IncomingDocumentNumber: %s", doc.IncomingDocumentNumber)
	}
	if doc.DocumentNumber != "2452" {
		t.Errorf("Неверный DocumentNumber: %s", doc.DocumentNumber)
	}
	if doc.SupplierUUID != "cfdd6790-d3bb-49a0-a55b-4178fa49fe8c" {
		t.Errorf("Неверный SupplierUUID: %s", doc.SupplierUUID)
	}
	if doc.Status != "PROCESSED" {
		t.Errorf("Неверный статус: %s", doc.Status)
	}

	if len(doc.Items) != 2 {
		t.Fatalf("Ожидалось 2 позиции, получено %d", len(doc.Items))
	}

	item1 := doc.Items[0]
	if item1.ProductArticle != "00598" || item1.Amount != 20 || item1.Sum != 14400 || item1.Price != 720 {
		t.Errorf("Некорректные параметры item1: %+v", item1)
	}

	item2 := doc.Items[1]
	if item2.ProductArticle != "00600" || item2.ActualAmount != 10.5 || item2.Sum != 1050 || item2.VatSum != 210 {
		t.Errorf("Некорректные параметры item2: %+v", item2)
	}
}

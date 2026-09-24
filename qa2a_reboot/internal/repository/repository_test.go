package repository

import (
	"strings"
	"testing"
	"time"
)

func TestSupplierSplitN(t *testing.T) {
	parseSupplier := func(supplier string) (string, string) {
		uuid := ""
		name := "Без поставщика"
		if supplier != "" {
			parts := strings.SplitN(supplier, "|", 2)
			if len(parts) == 2 {
				uuid = parts[0]
				name = parts[1]
			} else {
				name = supplier
			}
		}
		return uuid, name
	}

	tests := []struct {
		input    string
		wantUUID string
		wantName string
	}{
		{
			input:    "",
			wantUUID: "",
			wantName: "Без поставщика",
		},
		{
			input:    "supp-uuid-1|ООО Ромашка",
			wantUUID: "supp-uuid-1",
			wantName: "ООО Ромашка",
		},
		{
			input:    "supp-uuid-2|ООО \"Рога | Копыта\"",
			wantUUID: "supp-uuid-2",
			wantName: "ООО \"Рога | Копыта\"",
		},
		{
			input:    "ПростойПоставщик",
			wantUUID: "",
			wantName: "ПростойПоставщик",
		},
	}

	for _, tc := range tests {
		gotUUID, gotName := parseSupplier(tc.input)
		if gotUUID != tc.wantUUID || gotName != tc.wantName {
			t.Errorf("parseSupplier(%q) = (%q, %q); want (%q, %q)",
				tc.input, gotUUID, gotName, tc.wantUUID, tc.wantName)
		}
	}
}

func TestCalculateYektBusinessDate(t *testing.T) {
	loc := GetYekaterinburgLocation()

	tests := []struct {
		name          string
		inputTime     time.Time
		wantDate      string
		wantIsShift   bool
		wantDisplay   string
	}{
		{
			name:        "Early morning 02:30 YEKT belongs to previous day shift",
			inputTime:   time.Date(2026, 9, 20, 2, 30, 0, 0, loc),
			wantDate:    "2026-09-19",
			wantIsShift: true,
			wantDisplay: "2026-09-19 (Смена)",
		},
		{
			name:        "Boundary 05:59:59 YEKT belongs to previous day shift",
			inputTime:   time.Date(2026, 9, 20, 5, 59, 59, 0, loc),
			wantDate:    "2026-09-19",
			wantIsShift: true,
			wantDisplay: "2026-09-19 (Смена)",
		},
		{
			name:        "Cutoff 06:00:00 YEKT belongs to current calendar day",
			inputTime:   time.Date(2026, 9, 20, 6, 0, 0, 0, loc),
			wantDate:    "2026-09-20",
			wantIsShift: false,
			wantDisplay: "2026-09-20",
		},
		{
			name:        "Daytime 14:15:00 YEKT belongs to current calendar day",
			inputTime:   time.Date(2026, 9, 20, 14, 15, 0, 0, loc),
			wantDate:    "2026-09-20",
			wantIsShift: false,
			wantDisplay: "2026-09-20",
		},
		{
			name:        "Late night 23:59:59 YEKT belongs to current calendar day",
			inputTime:   time.Date(2026, 9, 20, 23, 59, 59, 0, loc),
			wantDate:    "2026-09-20",
			wantIsShift: false,
			wantDisplay: "2026-09-20",
		},
		{
			name:        "Midnight 00:00:00 YEKT belongs to previous day shift",
			inputTime:   time.Date(2026, 9, 21, 0, 0, 0, 0, loc),
			wantDate:    "2026-09-20",
			wantIsShift: true,
			wantDisplay: "2026-09-20 (Смена)",
		},
		{
			name:        "New year midnight shift: 2027-01-01 01:00 YEKT belongs to 2026-12-31",
			inputTime:   time.Date(2027, 1, 1, 1, 0, 0, 0, loc),
			wantDate:    "2026-12-31",
			wantIsShift: true,
			wantDisplay: "2026-12-31 (Смена)",
		},
		{
			name:        "UTC input: 2026-09-19 23:30:00 UTC is 2026-09-20 04:30:00 YEKT (shift)",
			inputTime:   time.Date(2026, 9, 19, 23, 30, 0, 0, time.UTC),
			wantDate:    "2026-09-19",
			wantIsShift: true,
			wantDisplay: "2026-09-19 (Смена)",
		},
		{
			name:        "UTC input: 2026-09-20 01:00:00 UTC is 2026-09-20 06:00:00 YEKT (current day)",
			inputTime:   time.Date(2026, 9, 20, 1, 0, 0, 0, time.UTC),
			wantDate:    "2026-09-20",
			wantIsShift: false,
			wantDisplay: "2026-09-20",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			bDate := CalculateYektBusinessDate(tc.inputTime)
			if bDate != tc.wantDate {
				t.Errorf("CalculateYektBusinessDate(%v) = %q; want %q",
					tc.inputTime, bDate, tc.wantDate)
			}

			bDateLegacy := CalculateBusinessDate(tc.inputTime)
			if bDateLegacy != tc.wantDate {
				t.Errorf("CalculateBusinessDate(%v) = %q; want %q",
					tc.inputTime, bDateLegacy, tc.wantDate)
			}
		})
	}
}


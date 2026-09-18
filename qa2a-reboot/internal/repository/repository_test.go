package repository

import (
	"strings"
	"testing"
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

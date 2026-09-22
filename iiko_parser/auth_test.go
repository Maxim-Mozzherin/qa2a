package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

func TestPasswordHashingAndVerification(t *testing.T) {
	initialPass := "!123Maxim.!"
	hash, err := bcrypt.GenerateFromPassword([]byte(initialPass), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("bcrypt.GenerateFromPassword failed: %v", err)
	}

	// Correct password must match
	if err := bcrypt.CompareHashAndPassword(hash, []byte(initialPass)); err != nil {
		t.Errorf("expected password to match hash, got error: %v", err)
	}

	// Wrong password must fail
	if err := bcrypt.CompareHashAndPassword(hash, []byte("wrong_password")); err == nil {
		t.Errorf("expected wrong password to fail")
	}
}

func TestTokenGeneration(t *testing.T) {
	generatedTokens := make(map[string]bool)
	for i := 0; i < 50; i++ {
		tokenBytes := make([]byte, 32)
		n, err := rand.Read(tokenBytes)
		if err != nil {
			t.Fatalf("rand.Read failed: %v", err)
		}
		if n != 32 {
			t.Fatalf("expected 32 bytes, got %d", n)
		}

		token := hex.EncodeToString(tokenBytes)
		if len(token) != 64 {
			t.Errorf("expected 64 hex characters, got %d", len(token))
		}
		if generatedTokens[token] {
			t.Fatalf("duplicate token generated: %s", token)
		}
		generatedTokens[token] = true
	}
}

func TestGetAuthUserByTokenEmpty(t *testing.T) {
	cases := []string{"", " ", "Bearer ", "Bearer   "}
	for _, tc := range cases {
		u, err := getAuthUserByToken(tc)
		if err == nil {
			t.Errorf("expected error for token %q, got user %+v", tc, u)
		}
	}
}

func TestCheckAccountantAccessInvalid(t *testing.T) {
	// Zero or negative company ID or empty token should immediately return false without DB queries
	if checkAccountantAccess("", 1) {
		t.Errorf("expected false for empty token")
	}
	if checkAccountantAccess("any_token", 0) {
		t.Errorf("expected false for companyID = 0")
	}
	if checkAccountantAccess("any_token", -5) {
		t.Errorf("expected false for negative companyID")
	}
}

func TestGetEnv(t *testing.T) {
	val := getEnv("NON_EXISTENT_ENV_KEY_12345", "fallback_default")
	if val != "fallback_default" {
		t.Errorf("expected fallback_default, got %s", val)
	}
}

func TestBearerTrimming(t *testing.T) {
	rawToken := "abcdef123456"
	withBearer := "Bearer " + rawToken
	trimmed := strings.TrimSpace(strings.TrimPrefix(withBearer, "Bearer "))
	if trimmed != rawToken {
		t.Errorf("expected %s, got %s", rawToken, trimmed)
	}
}

func TestContextWithAuthUserAndGetAuthUser(t *testing.T) {
	// 1. Nil request
	if GetAuthUser(nil) != nil {
		t.Errorf("expected nil for nil request")
	}

	// 2. Request without authUser in context
	req := httptest.NewRequest("GET", "/api/companies", nil)
	if GetAuthUser(req) != nil {
		t.Errorf("expected nil when context lacks authUser")
	}

	// 3. Request with valid AuthUser
	firmID := 42
	user := &AuthUser{
		ID:               10,
		AccountingFirmID: &firmID,
		Login:            "bugh",
		Role:             "superadmin",
	}

	ctx := ContextWithAuthUser(req.Context(), user)
	reqWithCtx := req.WithContext(ctx)

	extracted := GetAuthUser(reqWithCtx)
	if extracted == nil {
		t.Fatalf("expected extracted user, got nil")
	}
	if extracted.ID != 10 || extracted.Login != "bugh" || extracted.Role != "superadmin" || *extracted.AccountingFirmID != 42 {
		t.Errorf("mismatch in extracted user: %+v", extracted)
	}
}

func TestCheckAccountantAccessUser(t *testing.T) {
	// 1. Nil user
	if checkAccountantAccessUser(nil, 1) {
		t.Errorf("expected false for nil user")
	}

	// 2. Invalid companyID
	superUser := &AuthUser{ID: 1, Role: "superadmin"}
	if checkAccountantAccessUser(superUser, 0) {
		t.Errorf("expected false for companyID 0")
	}
	if checkAccountantAccessUser(superUser, -1) {
		t.Errorf("expected false for negative companyID")
	}

	// 3. Superadmin godmode access to any positive companyID
	if !checkAccountantAccessUser(superUser, 100) {
		t.Errorf("expected superadmin to have godmode access to company 100")
	}

	// 4. Global accountant godmode access to any positive companyID even with nil firm
	globalUser := &AuthUser{ID: 3, Role: "global_accountant", AccountingFirmID: nil}
	if !checkAccountantAccessUser(globalUser, 100) {
		t.Errorf("expected global_accountant to have godmode access to company 100")
	}

	// 5. Non-superadmin with nil firm ID
	accountantNilFirm := &AuthUser{ID: 2, Role: "accountant", AccountingFirmID: nil}
	if checkAccountantAccessUser(accountantNilFirm, 100) {
		t.Errorf("expected false for accountant without firm")
	}
}

func TestGlobalAccountantCannotGenerateAccountantInvite(t *testing.T) {
	req := httptest.NewRequest("POST", "/api/accountant-invite/generate", nil)
	globalUser := &AuthUser{ID: 3, Login: "buh", Role: "global_accountant", AccountingFirmID: nil}
	reqWithCtx := req.WithContext(ContextWithAuthUser(req.Context(), globalUser))

	rec := httptest.NewRecorder()
	handleGenerateAccountantInvite(rec, reqWithCtx)

	if rec.Code != http.StatusForbidden {
		t.Errorf("expected 403 Forbidden for global_accountant on accountant-invite/generate, got %d", rec.Code)
	}
}

func TestIikoIncomingInvoiceXMLMarshalling(t *testing.T) {
	doc := IikoIncomingInvoiceXML{
		IncomingDate:           "2026-09-17",
		Supplier:               "supplier-uuid-123 & co <test>",
		DefaultStore:           "store-uuid-456",
		DateIncoming:           "17.09.2026",
		UseDefaultDocumentTime: true,
		IncomingDocumentNumber: "№123 & 45/A<B> \"quotes\"",
		Status:                 "NEW",
		Items: []IikoIncomingInvoiceItemXML{
			{
				Amount:       "10.000",
				Product:      "product-uuid-789",
				Num:          1,
				Sum:          "1500.00",
				VatPercent:   "20.00",
				VatSum:       "250.00",
				Price:        "150.0000",
				Store:        "store-uuid-456",
				ActualAmount: "10.000",
			},
		},
	}

	xmlBytes, err := xml.Marshal(doc)
	if err != nil {
		t.Fatalf("xml.Marshal failed: %v", err)
	}

	xmlStr := string(xmlBytes)

	// Ensure special characters were safely escaped by encoding/xml
	if strings.Contains(xmlStr, "<B>") {
		t.Errorf("raw <B> unescaped in XML: %s", xmlStr)
	}
	if !strings.Contains(xmlStr, "&amp;") {
		t.Errorf("expected &amp; escaping in XML: %s", xmlStr)
	}
	if !strings.Contains(xmlStr, "&lt;") {
		t.Errorf("expected &lt; escaping in XML: %s", xmlStr)
	}

	// Ensure unmarshaling decodes the exact original text back
	var unmarshaledDoc IikoIncomingInvoiceXML
	if err := xml.Unmarshal(xmlBytes, &unmarshaledDoc); err != nil {
		t.Fatalf("xml.Unmarshal failed on generated XML: %v", err)
	}

	if unmarshaledDoc.IncomingDocumentNumber != doc.IncomingDocumentNumber {
		t.Errorf("expected IncomingDocumentNumber %q, got %q", doc.IncomingDocumentNumber, unmarshaledDoc.IncomingDocumentNumber)
	}
	if unmarshaledDoc.Supplier != doc.Supplier {
		t.Errorf("expected Supplier %q, got %q", doc.Supplier, unmarshaledDoc.Supplier)
	}
	if len(unmarshaledDoc.Items) != 1 || unmarshaledDoc.Items[0].Amount != "10.000" {
		t.Errorf("unexpected items in unmarshaled doc: %+v", unmarshaledDoc.Items)
	}
}

func TestHandleSaveTemplateProxyAuthorization(t *testing.T) {
	// 1. Accountant without firm / unauthorized company should receive 403 Forbidden
	bodyJSON := `{"store_uuid":"str-123","name":"Test Template","items":["it1","it2"],"company_id":999}`
	req := httptest.NewRequest("POST", "/api/templates/save", strings.NewReader(bodyJSON))
	user := &AuthUser{ID: 5, Login: "acc", Role: "accountant", AccountingFirmID: nil}
	reqWithCtx := req.WithContext(ContextWithAuthUser(req.Context(), user))

	rec := httptest.NewRecorder()
	handleSaveTemplateProxy(rec, reqWithCtx)

	if rec.Code != http.StatusForbidden {
		t.Errorf("expected 403 Forbidden, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "Доступ к сохранению бланков для данного заведения запрещен") {
		t.Errorf("unexpected body: %s", rec.Body.String())
	}
}

func TestHandleRejectUnlistedOperation_MethodAndValidation(t *testing.T) {
	// 1. Non-DELETE method must return 405 Method Not Allowed
	reqPost := httptest.NewRequest("POST", "/api/unlisted-operations/reject", nil)
	recPost := httptest.NewRecorder()
	handleRejectUnlistedOperation(recPost, reqPost)
	if recPost.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", recPost.Code)
	}

	// 2. Missing params must return 400 Bad Request
	reqDelNoParams := httptest.NewRequest("DELETE", "/api/unlisted-operations/reject", nil)
	recDelNoParams := httptest.NewRecorder()
	handleRejectUnlistedOperation(recDelNoParams, reqDelNoParams)
	if recDelNoParams.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", recDelNoParams.Code)
	}

	// 3. Unauthorized accountant must return 403 Forbidden
	reqDel := httptest.NewRequest("DELETE", "/api/unlisted-operations/reject?company_id=999&operation_id=123", nil)
	user := &AuthUser{ID: 10, Login: "acc", Role: "accountant", AccountingFirmID: nil}
	reqWithCtx := reqDel.WithContext(ContextWithAuthUser(reqDel.Context(), user))
	recDel := httptest.NewRecorder()
	handleRejectUnlistedOperation(recDel, reqWithCtx)
	if recDel.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", recDel.Code)
	}
}

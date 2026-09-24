package handlers

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestVerifySignedToken_Security(t *testing.T) {
	secret := "test-bot-secret-key-12345"
	tgID := int64(123456789)

	// 1. Valid token
	validToken := generateSignedToken(tgID, 1, secret)
	gotID := verifySignedToken(validToken, secret)
	if gotID != tgID {
		t.Errorf("expected tgID %d, got %d", tgID, gotID)
	}

	// 2. Tampered signature
	parts := strings.Split(validToken, ":")
	tamperedToken := fmt.Sprintf("%s:%s:%s:%s", parts[0], parts[1], parts[2], "bad_signature_00000000000000000000000000000000000000000000000000000000")
	if verifySignedToken(tamperedToken, secret) != 0 {
		t.Errorf("expected 0 for tampered signature token")
	}

	// 3. Expired token
	pastExp := time.Now().Add(-1 * time.Hour).Unix()
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(fmt.Sprintf("%d:%d:%d", tgID, 1, pastExp)))
	expiredSig := hex.EncodeToString(mac.Sum(nil))
	expiredToken := fmt.Sprintf("%d:%d:%d:%s", tgID, 1, pastExp, expiredSig)

	if verifySignedToken(expiredToken, secret) != 0 {
		t.Errorf("expected 0 for expired token")
	}

	// 4. Malformed token formats
	malformed := []string{"", "123", "123:456", "abc:def:ghi", "::: "}
	for _, m := range malformed {
		if verifySignedToken(m, secret) != 0 {
			t.Errorf("expected 0 for malformed token %q", m)
		}
	}
}

func TestValidateTelegramData_Security(t *testing.T) {
	botToken := "123456:ABC-DEF1234ghIkl-zyx57W2v1u123ew11"

	// 1. Empty bot token should return false
	if validateTelegramData("auth_date=1620000000&query_id=test&user=%7B%7D&hash=fake", "") {
		t.Errorf("expected false for empty bot token")
	}

	// 2. Empty initData should return false
	if validateTelegramData("", botToken) {
		t.Errorf("expected false for empty initData")
	}

	// 3. Construct properly signed Telegram WebApp initData
	authDate := fmt.Sprintf("%d", time.Now().Unix())
	queryID := "AAHdF6IQAAAAAN0XohDhrOrc"
	userJSON := `{"id":279058397,"first_name":"Vladislav","last_name":"K","username":"vladislav","language_code":"ru"}`

	dataCheckString := fmt.Sprintf("auth_date=%s\nquery_id=%s\nuser=%s", authDate, queryID, userJSON)

	secretKey := hmac.New(sha256.New, []byte("WebAppData"))
	secretKey.Write([]byte(botToken))

	mac := hmac.New(sha256.New, secretKey.Sum(nil))
	mac.Write([]byte(dataCheckString))
	validHash := hex.EncodeToString(mac.Sum(nil))

	validInitData := fmt.Sprintf("auth_date=%s&query_id=%s&user=%s&hash=%s",
		authDate, queryID, userJSON, validHash)

	if !validateTelegramData(validInitData, botToken) {
		t.Errorf("expected valid initData to verify successfully")
	}

	// 4. Tampered initData (modified user ID)
	tamperedInitData := fmt.Sprintf("auth_date=%s&query_id=%s&user=%s&hash=%s",
		authDate, queryID, `{"id":999999999}`, validHash)
	if validateTelegramData(tamperedInitData, botToken) {
		t.Errorf("expected tampered initData to fail verification")
	}
}

func TestCreateExternalTemplateHandler_Security(t *testing.T) {
	origKey := os.Getenv("EXTERNAL_API_KEY")
	os.Setenv("EXTERNAL_API_KEY", "test-secure-api-key-8099")
	defer os.Setenv("EXTERNAL_API_KEY", origKey)

	h := &Handler{}

	// 1. Missing Authorization header
	reqNoAuth := httptest.NewRequest("POST", "/api/external/inventory-templates", strings.NewReader(`{}`))
	recNoAuth := httptest.NewRecorder()
	h.CreateExternalTemplateHandler(recNoAuth, reqNoAuth)
	if recNoAuth.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 Unauthorized for missing auth, got %d", recNoAuth.Code)
	}

	// 2. Wrong token
	reqWrongAuth := httptest.NewRequest("POST", "/api/external/inventory-templates", strings.NewReader(`{}`))
	reqWrongAuth.Header.Set("Authorization", "Bearer invalid-token-xyz")
	recWrongAuth := httptest.NewRecorder()
	h.CreateExternalTemplateHandler(recWrongAuth, reqWrongAuth)
	if recWrongAuth.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 Unauthorized for wrong token, got %d", recWrongAuth.Code)
	}

	// 3. Empty Bearer token
	reqEmptyBearer := httptest.NewRequest("POST", "/api/external/inventory-templates", strings.NewReader(`{}`))
	reqEmptyBearer.Header.Set("Authorization", "Bearer ")
	recEmptyBearer := httptest.NewRecorder()
	h.CreateExternalTemplateHandler(recEmptyBearer, reqEmptyBearer)
	if recEmptyBearer.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 Unauthorized for empty Bearer token, got %d", recEmptyBearer.Code)
	}
}

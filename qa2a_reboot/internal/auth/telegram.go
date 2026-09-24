package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// MaxInitDataAge — максимальный срок жизни initData для защиты от replay attacks (24 часа).
const MaxInitDataAge = 24 * time.Hour

// ValidateInitData проверяет криптографическую подпись Telegram Mini App initData с использованием токена бота.
func ValidateInitData(initData, token string) bool {
	return ValidateInitDataWithMaxAge(initData, token, MaxInitDataAge)
}

// ValidateInitDataWithMaxAge проверяет подпись и проверяет, что auth_date не старше maxAge.
// Если maxAge <= 0, проверка срока давности не выполняется.
func ValidateInitDataWithMaxAge(initData, token string, maxAge time.Duration) bool {
	if token == "" || initData == "" {
		return false
	}

	params, err := url.ParseQuery(initData)
	if err != nil {
		return false
	}

	hash := params.Get("hash")
	if hash == "" {
		return false
	}
	params.Del("hash")

	// Проверка срока жизни initData (защита от replay attack)
	if maxAge > 0 {
		authDateStr := params.Get("auth_date")
		if authDateStr != "" {
			if authTimestamp, err := strconv.ParseInt(authDateStr, 10, 64); err == nil {
				authTime := time.Unix(authTimestamp, 0)
				// Если время в будущем с запасом > 5 мин или старше maxAge
				if time.Since(authTime) > maxAge || time.Until(authTime) > 5*time.Minute {
					return false
				}
			}
		}
	}

	var keys []string
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var data []string
	for _, k := range keys {
		data = append(data, k+"="+params.Get(k))
	}
	dataCheckString := strings.Join(data, "\n")

	mac := hmac.New(sha256.New, []byte("WebAppData"))
	mac.Write([]byte(token))
	secret := mac.Sum(nil)

	h := hmac.New(sha256.New, secret)
	h.Write([]byte(dataCheckString))
	expected := hex.EncodeToString(h.Sum(nil))
	return hmac.Equal([]byte(expected), []byte(hash))
}

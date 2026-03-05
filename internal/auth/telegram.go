package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"sort"
	"strings"
)

func ValidateInitData(initData, token string) bool {
	params, _ := url.ParseQuery(initData)
	hash := params.Get("hash")
	params.Del("hash")

	var keys []string
	for k := range params { keys = append(keys, k) }
	sort.Strings(keys)

	var data []string
	for _, k := range keys { data = append(data, k+"="+params.Get(k)) }
	dataCheckString := strings.Join(data, "\n")

	mac := hmac.New(sha256.New, []byte("WebAppData"))
	mac.Write([]byte(token))
	secret := mac.Sum(nil)

	h := hmac.New(sha256.New, secret)
	h.Write([]byte(dataCheckString))
	return hex.EncodeToString(h.Sum(nil)) == hash
}

package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"

	"golang.org/x/crypto/pbkdf2"
)

const (
	pbkdf2Salt       = "qa2a-aead-salt-2026-v1"
	pbkdf2Iterations = 100000
	keyLen           = 32
)

func deriveKey(passphrase string) []byte {
	return pbkdf2.Key([]byte(passphrase), []byte(pbkdf2Salt), pbkdf2Iterations, keyLen, sha256.New)
}

func deriveKeyLegacy(passphrase string) []byte {
	hash := sha256.Sum256([]byte(passphrase))
	return hash[:]
}

// Encrypt шифрует открытый текст с помощью AES-GCM (PBKDF2-деривация ключа)
func Encrypt(plainText, secret string) (string, error) {
	if plainText == "" {
		return "", nil
	}
	if secret == "" {
		return "", errors.New("секретный ключ шифрования пуст")
	}

	key := deriveKey(secret)
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("ошибка генерации nonce: %w", err)
	}
	ciphertext := gcm.Seal(nonce, nonce, []byte(plainText), nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

func Decrypt(cryptoText, secret string) (string, error) {
	if cryptoText == "" {
		return "", nil
	}
	if secret == "" {
		return "", errors.New("секретный ключ шифрования пуст")
	}

	data, err := base64.StdEncoding.DecodeString(cryptoText)
	if err != nil {
		return "", fmt.Errorf("ошибка base64: %w", err)
	}

	key := deriveKey(secret)
	block, err := aes.NewCipher(key)
	if err == nil {
		gcm, errGCM := cipher.NewGCM(block)
		if errGCM == nil && len(data) >= gcm.NonceSize() {
			nonceSize := gcm.NonceSize()
			nonce, ciphertext := data[:nonceSize], data[nonceSize:]
			if plainText, errOpen := gcm.Open(nil, nonce, ciphertext, nil); errOpen == nil {
				return string(plainText), nil
			}
		}
	}

	// Fallback к legacy SHA-256
	legacyKey := deriveKeyLegacy(secret)
	legacyBlock, err := aes.NewCipher(legacyKey)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(legacyBlock)
	if err != nil {
		return "", err
	}
	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return "", errors.New("неверная длина шифротекста")
	}
	nonce, ciphertext := data[:nonceSize], data[nonceSize:]
	plainText, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("ошибка дешифрования: %w", err)
	}
	return string(plainText), nil
}

func HashPasswordSHA1(password string) string {
	hasher := sha1.New()
	hasher.Write([]byte(password))
	return hex.EncodeToString(hasher.Sum(nil))
}

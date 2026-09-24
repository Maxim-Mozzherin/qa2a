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

var (
	// ErrEmptySecretKey возвращается при попытке шифрования с пустым ключом.
	ErrEmptySecretKey = errors.New("мастер-ключ шифрования не может быть пустым")

	// ErrInvalidCiphertext возвращается при поврежденных или усеченных зашифрованных данных.
	ErrInvalidCiphertext = errors.New("длина шифротекста меньше размера вектора инициализации (nonce)")
)

const (
	pbkdf2Salt       = "qa2a-aead-salt-2026-v1"
	pbkdf2Iterations = 100000
	keyLen           = 32
)

// deriveKey преобразует мастер-ключ в 32-байтовый ключ (256 бит)
// с помощью PBKDF2 (HMAC-SHA256, 100 000 итераций, криптостойкая соль).
func deriveKey(passphrase string) []byte {
	return pbkdf2.Key([]byte(passphrase), []byte(pbkdf2Salt), pbkdf2Iterations, keyLen, sha256.New)
}

// deriveKeyLegacy сохранен для плавной миграции данных, зашифрованных с помощью одиночного SHA-256.
func deriveKeyLegacy(passphrase string) []byte {
	hash := sha256.Sum256([]byte(passphrase))
	return hash[:]
}

// Encrypt выполняет симметричное шифрование открытого текста алгоритмом AES-256 в режиме GCM.
// Результат кодируется в стандартный Base64 с префиксом в виде случайного Nonce (вектора инициализации).
func Encrypt(plainText, secret string) (string, error) {
	if plainText == "" {
		return "", nil
	}
	if secret == "" {
		return "", ErrEmptySecretKey
	}

	key := deriveKey(secret)
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("ошибка инициализации AES шифра: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("ошибка создания режима GCM: %w", err)
	}

	// Генерируем уникальный Nonce для каждого вызова шифрования
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("ошибка генерации криптографического nonce: %w", err)
	}

	// Seal добавляет зашифрованный текст к nonce и вычисляет аутентификационный тег (AEAD)
	cipherText := gcm.Seal(nonce, nonce, []byte(plainText), nil)
	return base64.StdEncoding.EncodeToString(cipherText), nil
}

// Decrypt принимает Base64-строку, извлекает Nonce и расшифровывает открытый текст.
func Decrypt(cryptoText, secret string) (string, error) {
	if cryptoText == "" {
		return "", nil
	}
	if secret == "" {
		return "", ErrEmptySecretKey
	}

	data, err := base64.StdEncoding.DecodeString(cryptoText)
	if err != nil {
		return "", fmt.Errorf("ошибка декодирования Base64: %w", err)
	}

	// 1. Попытка расшифровать с помощью актуального PBKDF2 ключа
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

	// 2. Fallback: расшифрование с помощью legacy SHA-256 ключа
	legacyKey := deriveKeyLegacy(secret)
	legacyBlock, err := aes.NewCipher(legacyKey)
	if err != nil {
		return "", fmt.Errorf("ошибка инициализации AES шифра: %w", err)
	}

	gcm, err := cipher.NewGCM(legacyBlock)
	if err != nil {
		return "", fmt.Errorf("ошибка создания режима GCM: %w", err)
	}

	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return "", ErrInvalidCiphertext
	}

	nonce, ciphertext := data[:nonceSize], data[nonceSize:]
	plainText, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("ошибка проверки целостности/расшифрования данных (неверный ключ?): %w", err)
	}

	return string(plainText), nil
}

// HashPasswordSHA1 вычисляет SHA-1 хэш строки в шестнадцатеричном представлении.
// Требуется протоколом авторизации iiko RMS API (/resto/api/auth?login=...&pass=...).
func HashPasswordSHA1(password string) string {
	hasher := sha1.New()
	hasher.Write([]byte(password))
	return hex.EncodeToString(hasher.Sum(nil))
}


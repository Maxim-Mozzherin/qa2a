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
)

var (
	// ErrEmptySecretKey возвращается при попытке выполнения криптографической операции с пустым ключом.
	ErrEmptySecretKey = errors.New("мастер-ключ шифрования не может быть пустым")

	// ErrInvalidCiphertext возвращается при поврежденных или усеченных зашифрованных данных.
	ErrInvalidCiphertext = errors.New("длина шифротекста меньше размера вектора инициализации (nonce)")
)

// deriveKey преобразует мастер-ключ любой длины в строго 32-байтовый ключ (256 бит) с помощью SHA-256.
func deriveKey(passphrase string) []byte {
	hash := sha256.Sum256([]byte(passphrase))
	return hash[:]
}

// Encrypt выполняет симметричное шифрование открытого текста алгоритмом AES-256 в режиме GCM.
// Результат упаковывается в стандартную Base64-строку с префиксом Nonce (вектором инициализации).
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

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("ошибка генерации криптографического nonce: %w", err)
	}

	cipherText := gcm.Seal(nonce, nonce, []byte(plainText), nil)
	return base64.StdEncoding.EncodeToString(cipherText), nil
}

// Decrypt принимает зашифрованную Base64-строку и мастер-ключ,
// возвращая исходный расшифрованный пароль API iiko RMS.
func Decrypt(cryptoText, secret string) (string, error) {
	if cryptoText == "" {
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

	// Декодируем Base64 строку
	data, err := base64.StdEncoding.DecodeString(cryptoText)
	if err != nil {
		return "", fmt.Errorf("ошибка декодирования Base64: %w", err)
	}

	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return "", ErrInvalidCiphertext
	}

	// Извлекаем вектор инициализации (nonce) и зашифрованную часть
	nonce, ciphertext := data[:nonceSize], data[nonceSize:]

	// Расшифровываем и верифицируем тег целостности
	plainText, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("ошибка расшифрования данных (неверный мастер-ключ?): %w", err)
	}

	return string(plainText), nil
}

// HashPasswordSHA1 вычисляет SHA-1 хэш пароля для авторизации в REST API iiko RMS (/resto/api/auth).
func HashPasswordSHA1(password string) string {
	hasher := sha1.New()
	hasher.Write([]byte(password))
	return hex.EncodeToString(hasher.Sum(nil))
}
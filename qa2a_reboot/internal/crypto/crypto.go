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
	// ErrEmptySecretKey возвращается при попытке шифрования с пустым ключом.
	ErrEmptySecretKey = errors.New("мастер-ключ шифрования не может быть пустым")

	// ErrInvalidCiphertext возвращается при поврежденных или усеченных зашифрованных данных.
	ErrInvalidCiphertext = errors.New("длина шифротекста меньше размера вектора инициализации (nonce)")
)

// deriveKey преобразует любую пользовательскую строку-секрет из .env
// в строго 32-байтовый криптографический ключ (256 бит) через SHA-256.
func deriveKey(passphrase string) []byte {
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

	key := deriveKey(secret)
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("ошибка инициализации AES шифра: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("ошибка создания режима GCM: %w", err)
	}

	data, err := base64.StdEncoding.DecodeString(cryptoText)
	if err != nil {
		return "", fmt.Errorf("ошибка декодирования Base64: %w", err)
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


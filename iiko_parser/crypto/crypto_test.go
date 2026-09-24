package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"io"
	"testing"
)

func TestEncryptDecryptPBKDF2(t *testing.T) {
	secret := "my-very-strong-production-master-secret-2026"
	plainText := "SuperP@ssw0rd!iikoRMS#999"

	encrypted, err := Encrypt(plainText, secret)
	if err != nil {
		t.Fatalf("unexpected Encrypt error: %v", err)
	}

	decrypted, err := Decrypt(encrypted, secret)
	if err != nil {
		t.Fatalf("unexpected Decrypt error: %v", err)
	}

	if decrypted != plainText {
		t.Errorf("decrypted text mismatch: got %q, want %q", decrypted, plainText)
	}
}

func TestLegacyDecryptFallback(t *testing.T) {
	secret := "old-legacy-secret-key-32b-length!"
	plainText := "LegacyRestaurantPassword123"

	// 1. Manually encrypt using legacy single-round SHA-256 key derivation
	legacyHash := sha256.Sum256([]byte(secret))
	legacyKey := legacyHash[:]

	block, err := aes.NewCipher(legacyKey)
	if err != nil {
		t.Fatalf("failed to create legacy cipher: %v", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatalf("failed to create legacy GCM: %v", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		t.Fatalf("failed to generate nonce: %v", err)
	}
	legacyCipher := gcm.Seal(nonce, nonce, []byte(plainText), nil)
	legacyBase64 := base64.StdEncoding.EncodeToString(legacyCipher)

	// 2. Decrypt with new Decrypt function (which must fallback to legacy key)
	decrypted, err := Decrypt(legacyBase64, secret)
	if err != nil {
		t.Fatalf("Decrypt failed on legacy ciphertext: %v", err)
	}

	if decrypted != plainText {
		t.Errorf("legacy decryption mismatch: got %q, want %q", decrypted, plainText)
	}
}

func TestDecryptWithWrongKey(t *testing.T) {
	secret := "correct-secret-key-with-proper-length-32"
	wrongSecret := "wrong-secret-key-with-proper-length-32!!"
	plainText := "SensitiveData"

	encrypted, err := Encrypt(plainText, secret)
	if err != nil {
		t.Fatalf("unexpected Encrypt error: %v", err)
	}

	_, err = Decrypt(encrypted, wrongSecret)
	if err == nil {
		t.Errorf("expected error when decrypting with wrong key, got nil")
	}
}

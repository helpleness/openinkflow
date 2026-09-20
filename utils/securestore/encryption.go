package securestore

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"
)

const encryptedValuePrefix = "inkflow:v1:"

// EncryptWithSecret returns a versioned AES-256-GCM payload. It is intended for
// the rare server deployments where no operating-system credential manager is
// available; callers remain responsible for keeping the deployment secret out
// of source control and database backups.
func EncryptWithSecret(secret string, value []byte) (string, error) {
	if strings.TrimSpace(secret) == "" {
		return "", errors.New("credential encryption secret is empty")
	}
	if len(value) == 0 {
		return "", errors.New("credential value is empty")
	}
	block, err := aes.NewCipher(encryptionKey(secret))
	if err != nil {
		return "", fmt.Errorf("create credential cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("create credential cipher mode: %w", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("generate credential nonce: %w", err)
	}
	payload := gcm.Seal(nonce, nonce, value, nil)
	return encryptedValuePrefix + base64.RawURLEncoding.EncodeToString(payload), nil
}

// DecryptWithSecret decodes a payload written by EncryptWithSecret.
func DecryptWithSecret(secret, value string) ([]byte, error) {
	if strings.TrimSpace(secret) == "" {
		return nil, errors.New("credential encryption secret is empty")
	}
	if !strings.HasPrefix(value, encryptedValuePrefix) {
		return nil, errors.New("credential payload format is not supported")
	}
	payload, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(value, encryptedValuePrefix))
	if err != nil {
		return nil, fmt.Errorf("decode credential payload: %w", err)
	}
	block, err := aes.NewCipher(encryptionKey(secret))
	if err != nil {
		return nil, fmt.Errorf("create credential cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create credential cipher mode: %w", err)
	}
	if len(payload) < gcm.NonceSize() {
		return nil, errors.New("credential payload is truncated")
	}
	plain, err := gcm.Open(nil, payload[:gcm.NonceSize()], payload[gcm.NonceSize():], nil)
	if err != nil {
		return nil, errors.New("credential payload cannot be authenticated")
	}
	return plain, nil
}

func encryptionKey(secret string) []byte {
	sum := sha256.Sum256([]byte("InkFlow model credential encryption\x00" + secret))
	return sum[:]
}

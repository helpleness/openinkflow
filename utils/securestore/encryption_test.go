package securestore

import (
	"bytes"
	"strings"
	"testing"
)

func TestEncryptWithSecretRoundTripsAndUsesRandomNonces(t *testing.T) {
	secret := "deployment-secret"
	plain := []byte("sk-example-model-key")
	first, err := EncryptWithSecret(secret, plain)
	if err != nil {
		t.Fatal(err)
	}
	second, err := EncryptWithSecret(secret, plain)
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("encrypted values should use unique nonces")
	}
	if strings.Contains(first, string(plain)) {
		t.Fatalf("encrypted payload includes plaintext: %q", first)
	}
	decoded, err := DecryptWithSecret(secret, first)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(decoded, plain) {
		t.Fatalf("decoded = %q, want %q", decoded, plain)
	}
}

func TestDecryptWithSecretRejectsDifferentSecretAndTampering(t *testing.T) {
	encrypted, err := EncryptWithSecret("correct-secret", []byte("credential"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecryptWithSecret("other-secret", encrypted); err == nil {
		t.Fatal("decrypt with a different secret unexpectedly succeeded")
	}
	replacement := "A"
	if strings.HasSuffix(encrypted, replacement) {
		replacement = "B"
	}
	tampered := encrypted[:len(encrypted)-1] + replacement
	if _, err := DecryptWithSecret("correct-secret", tampered); err == nil {
		t.Fatal("decrypt of a tampered payload unexpectedly succeeded")
	}
}

package utils

import (
	"crypto/rand"
	"encoding/hex"
	"strings"
	"unicode/utf8"
)

// PrefixRunes returns at most n leading runes after trimming surrounding space.
func PrefixRunes(s string, n int) string {
	s = strings.TrimSpace(s)
	if n <= 0 || s == "" {
		return ""
	}
	rs := []rune(s)
	if len(rs) <= n {
		return s
	}
	return string(rs[:n])
}

// FirstNonEmpty returns the first value whose trimmed form is non-empty. It is
// useful for configuration fallbacks where an explicit value must win over a
// default, but whitespace-only values should be treated as unset.
func FirstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

// NormalizeText makes external text safe for persistence. PostgreSQL rejects
// NUL bytes in text/varchar values, while JSON can legally contain one as
// \u0000. Invalid UTF-8 is also normalized before it reaches the database.
func NormalizeText(value string) string {
	value = strings.ToValidUTF8(value, "\uFFFD")
	value = strings.ReplaceAll(value, "\x00", "")
	return strings.TrimSpace(value)
}

// Truncate keeps a UTF-8-safe prefix for logs and error messages.
func Truncate(value string, max int) string {
	if max <= 0 {
		return ""
	}
	if len(value) <= max {
		return value
	}
	prefix := value[:max]
	for len(prefix) > 0 && !utf8.ValidString(prefix) {
		prefix = prefix[:len(prefix)-1]
	}
	return prefix + "..."
}

func RandomStateID() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

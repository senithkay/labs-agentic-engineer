package services

import (
	"crypto/rand"
	"strings"
)

// sanitizeIdentifier lowercases name and removes any character that is not
// alphanumeric or underscore, capping the result at 63 characters.
func sanitizeIdentifier(name string) string {
	id := strings.ToLower(strings.ReplaceAll(name, "-", "_"))
	var sb strings.Builder
	for _, c := range id {
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '_' {
			sb.WriteRune(c)
		}
	}
	result := sb.String()
	if len(result) > 63 {
		result = result[:63]
	}
	return result
}

// randString returns a cryptographically random alphanumeric string of length n.
func randString(n int) (string, error) {
	const letters = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	for i := range b {
		b[i] = letters[int(b[i])%len(letters)]
	}
	return string(b), nil
}

package config

import (
	"crypto/subtle"
	"fmt"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

func HashPassword(password string) (string, error) {
	trimmed := strings.TrimSpace(password)
	if trimmed == "" {
		return "", fmt.Errorf("password cannot be empty")
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(trimmed), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("hash password: %w", err)
	}

	return string(hash), nil
}

func IsPasswordHashed(password string) bool {
	return strings.HasPrefix(password, "$2a$") || strings.HasPrefix(password, "$2b$") || strings.HasPrefix(password, "$2y$")
}

func CheckPassword(candidate, stored string) bool {
	if stored == "" {
		return false
	}

	if IsPasswordHashed(stored) {
		return bcrypt.CompareHashAndPassword([]byte(stored), []byte(candidate)) == nil
	}

	return subtle.ConstantTimeCompare([]byte(stored), []byte(candidate)) == 1
}

func NormalizeUsers(users map[string]UserAccount) error {
	for username, user := range users {
		user.Role = normalizeRole(user.Role)

		if !IsPasswordHashed(user.Password) {
			hash, err := HashPassword(user.Password)
			if err != nil {
				return fmt.Errorf("normalize user %s: %w", username, err)
			}
			user.Password = hash
		}

		users[username] = user
	}

	return nil
}

func normalizeRole(role string) string {
	normalized := strings.ToLower(strings.TrimSpace(role))
	if normalized == "" {
		return "technician"
	}

	return normalized
}

package config

import "testing"

func TestCheckPasswordSupportsLegacyAndHashedValues(t *testing.T) {
	t.Parallel()

	hash, err := HashPassword("secret123")
	if err != nil {
		t.Fatalf("HashPassword() error = %v", err)
	}

	if !CheckPassword("secret123", hash) {
		t.Fatal("expected bcrypt hash to validate")
	}

	if !CheckPassword("legacy-pass", "legacy-pass") {
		t.Fatal("expected legacy plaintext password to validate")
	}

	if CheckPassword("wrong", hash) {
		t.Fatal("expected wrong password to fail")
	}
}

func TestNormalizeUsersHashesPasswordsAndNormalizesRoles(t *testing.T) {
	t.Parallel()

	users := map[string]UserAccount{
		"Admin": {
			Password: "admin",
			Role:     " SUPERADMIN ",
		},
		"tech": {
			Password: "tech-pass",
		},
	}

	if err := NormalizeUsers(users); err != nil {
		t.Fatalf("NormalizeUsers() error = %v", err)
	}

	admin := users["Admin"]
	if !IsPasswordHashed(admin.Password) {
		t.Fatal("expected admin password to be hashed")
	}
	if admin.Role != "superadmin" {
		t.Fatalf("expected normalized role superadmin, got %q", admin.Role)
	}

	tech := users["tech"]
	if !IsPasswordHashed(tech.Password) {
		t.Fatal("expected technician password to be hashed")
	}
	if tech.Role != "technician" {
		t.Fatalf("expected default technician role, got %q", tech.Role)
	}

	if !CheckPassword("admin", admin.Password) {
		t.Fatal("expected normalized admin password to validate")
	}
	if !CheckPassword("tech-pass", tech.Password) {
		t.Fatal("expected normalized technician password to validate")
	}
	if CheckPassword("wrong", tech.Password) {
		t.Fatal("expected wrong normalized password to fail")
	}
}

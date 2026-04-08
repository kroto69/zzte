package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"olt-monitor/internal/config"
)

func TestLoginAcceptsHashedPassword(t *testing.T) {
	hashedPassword, err := config.HashPassword("secret123")
	if err != nil {
		t.Fatalf("HashPassword() error = %v", err)
	}

	handler := NewAuthHandler(&config.Config{
		Users: map[string]config.UserAccount{
			"alice": {Password: hashedPassword, Role: "technician"},
		},
	}, nil)

	body, err := json.Marshal(LoginRequest{Username: "alice", Password: "secret123"})
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/auth/login", bytes.NewReader(body))
	req.RemoteAddr = "127.0.0.1:12345"
	rr := httptest.NewRecorder()

	handler.Login(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var response Response
	if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if !response.Success {
		t.Fatalf("expected success response, got %+v", response)
	}

	data, ok := response.Data.(map[string]interface{})
	if !ok {
		t.Fatalf("expected response data map, got %T", response.Data)
	}
	if data["token"] == "" {
		t.Fatalf("expected token in response, got %+v", data)
	}
}

func TestChangePasswordHashesNewPassword(t *testing.T) {
	hashedPassword, err := config.HashPassword("old-pass")
	if err != nil {
		t.Fatalf("HashPassword() error = %v", err)
	}

	configDir := t.TempDir()
	configFile := filepath.Join(configDir, "olt_config.yaml")
	configContent := []byte("server:\n  host: 127.0.0.1\n  port: 8080\nusers:\n  alice:\n    password: " + hashedPassword + "\n    role: technician\n")
	if err := os.WriteFile(configFile, configContent, 0o600); err != nil {
		t.Fatalf("os.WriteFile() error = %v", err)
	}

	cfg, err := config.Load(configDir)
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	handler := NewAuthHandler(cfg, nil)

	body, err := json.Marshal(ChangePasswordRequest{OldPassword: "old-pass", NewPassword: "new-secret"})
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/auth/change-password", bytes.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), userContextKey, UserContext{Username: "alice", Role: "technician"}))
	rr := httptest.NewRecorder()

	handler.ChangePassword(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rr.Code, rr.Body.String())
	}

	stored := cfg.Users["alice"].Password
	if stored == "new-secret" {
		t.Fatal("expected stored password to be hashed, got plaintext")
	}
	if !config.CheckPassword("new-secret", stored) {
		t.Fatal("expected new password hash to validate")
	}
	if config.CheckPassword("old-pass", stored) {
		t.Fatal("expected old password to no longer validate")
	}
}

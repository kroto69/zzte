package handler

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"

	"olt-monitor/internal/config"
)

// UserHandler handles user management requests (superadmin only)
type UserHandler struct {
	cfg *config.Config
}

// NewUserHandler creates a new UserHandler
func NewUserHandler(cfg *config.Config) *UserHandler {
	return &UserHandler{cfg: cfg}
}

type UserResponse struct {
	Username string `json:"username"`
	Role     string `json:"role"`
}

type UserCreateRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Role     string `json:"role"`
}

type UserUpdateRequest struct {
	Password string `json:"password"`
	Role     string `json:"role"`
}

// ListUsers returns all users (without passwords)
func (h *UserHandler) ListUsers(w http.ResponseWriter, r *http.Request) {
	users := make([]UserResponse, 0, len(h.cfg.Users))
	for username, u := range h.cfg.Users {
		role := u.Role
		if role == "" {
			role = "technician"
		}
		users = append(users, UserResponse{
			Username: username,
			Role:     role,
		})
	}
	Success(w, users)
}

// CreateUser creates a new user
func (h *UserHandler) CreateUser(w http.ResponseWriter, r *http.Request) {
	var req UserCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		BadRequest(w, "Invalid request body")
		return
	}

	req.Username = strings.TrimSpace(req.Username)
	req.Role = strings.ToLower(strings.TrimSpace(req.Role))
	if req.Username == "" || req.Password == "" {
		BadRequest(w, "Username and password are required")
		return
	}
	if req.Role == "" {
		req.Role = "technician"
	}
	if req.Role != "superadmin" && req.Role != "technician" {
		BadRequest(w, "Invalid role")
		return
	}

	if _, exists := h.cfg.Users[req.Username]; exists {
		Conflict(w, "User already exists")
		return
	}

	hashedPassword, err := config.HashPassword(req.Password)
	if err != nil {
		BadRequest(w, err.Error())
		return
	}

	h.cfg.Users[req.Username] = config.UserAccount{
		Password: hashedPassword,
		Role:     req.Role,
	}
	if err := h.cfg.Save(); err != nil {
		log.Error().Err(err).Msg("Failed to save config after creating user")
		InternalError(w, "Failed to save configuration")
		return
	}

	Created(w, UserResponse{Username: req.Username, Role: req.Role})
}

// UpdateUser updates an existing user (role and/or password)
func (h *UserHandler) UpdateUser(w http.ResponseWriter, r *http.Request) {
	username := chi.URLParam(r, "username")
	if username == "" {
		BadRequest(w, "Username is required")
		return
	}

	var req UserUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		BadRequest(w, "Invalid request body")
		return
	}

	req.Role = strings.ToLower(strings.TrimSpace(req.Role))
	user, ok := h.cfg.Users[username]
	if !ok {
		NotFound(w, "User not found")
		return
	}

	if req.Password == "" && req.Role == "" {
		BadRequest(w, "No changes provided")
		return
	}

	if req.Role != "" && req.Role != "superadmin" && req.Role != "technician" {
		BadRequest(w, "Invalid role")
		return
	}

	// Prevent removing last superadmin
	if req.Role == "technician" && user.Role == "superadmin" && h.countSuperadmins() <= 1 {
		BadRequest(w, "Cannot remove last superadmin")
		return
	}

	if req.Password != "" {
		hashedPassword, err := config.HashPassword(req.Password)
		if err != nil {
			BadRequest(w, err.Error())
			return
		}
		user.Password = hashedPassword
	}
	if req.Role != "" {
		user.Role = strings.ToLower(strings.TrimSpace(req.Role))
	}

	h.cfg.Users[username] = user
	if err := h.cfg.Save(); err != nil {
		log.Error().Err(err).Msg("Failed to save config after updating user")
		InternalError(w, "Failed to save configuration")
		return
	}

	Success(w, UserResponse{Username: username, Role: user.Role})
}

// DeleteUser deletes a user
func (h *UserHandler) DeleteUser(w http.ResponseWriter, r *http.Request) {
	username := chi.URLParam(r, "username")
	if username == "" {
		BadRequest(w, "Username is required")
		return
	}

	// Prevent deleting self
	if current, ok := GetUserFromContext(r.Context()); ok && current.Username == username {
		BadRequest(w, "Cannot delete current user")
		return
	}

	user, ok := h.cfg.Users[username]
	if !ok {
		NotFound(w, "User not found")
		return
	}

	// Prevent deleting last superadmin
	if user.Role == "superadmin" && h.countSuperadmins() <= 1 {
		BadRequest(w, "Cannot delete last superadmin")
		return
	}

	delete(h.cfg.Users, username)
	if err := h.cfg.Save(); err != nil {
		log.Error().Err(err).Msg("Failed to save config after deleting user")
		InternalError(w, "Failed to save configuration")
		return
	}

	MessageResponse(w, "User deleted successfully")
}

func (h *UserHandler) countSuperadmins() int {
	count := 0
	for _, u := range h.cfg.Users {
		if strings.ToLower(u.Role) == "superadmin" {
			count++
		}
	}
	return count
}

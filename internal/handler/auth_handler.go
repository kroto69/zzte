package handler

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/golang-jwt/jwt/v5"
	"github.com/rs/zerolog/log"

	"olt-monitor/internal/config"
	"olt-monitor/internal/service"
)

// AuthHandler handles authentication requests
type AuthHandler struct {
	cfg      *config.Config
	mu       sync.Mutex
	rate     map[string]rateState
	lockouts map[string]lockoutState
	activity *service.ActivityService
}

// NewAuthHandler creates a new AuthHandler
func NewAuthHandler(cfg *config.Config, activity *service.ActivityService) *AuthHandler {
	currentJWTKey(cfg)

	return &AuthHandler{
		cfg:      cfg,
		rate:     make(map[string]rateState),
		lockouts: make(map[string]lockoutState),
		activity: activity,
	}
}

type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type ChangePasswordRequest struct {
	OldPassword string `json:"oldPassword"`
	NewPassword string `json:"newPassword"`
}

type rateState struct {
	count       int
	windowStart time.Time
}

type lockoutState struct {
	failed      int
	lockedUntil time.Time
}

const (
	loginFailLimit   = 5
	lockoutDuration  = 5 * time.Minute
	rateLimitWindow  = 1 * time.Minute
	rateLimitMaxHits = 20
)

var (
	jwtKey     []byte
	jwtKeyOnce sync.Once
)

type Claims struct {
	Username string `json:"username"`
	Role     string `json:"role"`
	jwt.RegisteredClaims
}

type contextKey string

const userContextKey contextKey = "user"

type UserContext struct {
	Username string
	Role     string
}

// GetUserFromContext returns user info stored by AuthMiddleware.
func GetUserFromContext(ctx context.Context) (UserContext, bool) {
	user, ok := ctx.Value(userContextKey).(UserContext)
	return user, ok
}

// Login authenticates a user and returns a JWT token
func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		BadRequest(w, "Invalid request payload")
		return
	}

	req.Username = strings.TrimSpace(req.Username)
	clientIP := getClientIP(r)

	if limited, _ := h.checkRateLimit(clientIP); limited {
		TooManyRequests(w, "Too many login attempts. Try again later.")
		return
	}

	if locked, _ := h.isLocked(req.Username); locked {
		TooManyRequests(w, "Account temporarily locked. Try again in 5 minutes.")
		return
	}

	storedUser, ok := h.cfg.Users[req.Username]
	if !ok || !config.CheckPassword(req.Password, storedUser.Password) {
		h.registerFailure(req.Username)
		logActivity(h.activity, r, "auth.login_failed", req.Username, map[string]interface{}{
			"ip": clientIP,
		})
		Unauthorized(w, "Invalid credentials")
		return
	}
	h.resetFailures(req.Username)
	role := storedUser.Role
	if role == "" {
		role = "technician"
	}

	expirationTime := time.Now().Add(24 * time.Hour)
	claims := &Claims{
		Username: req.Username,
		Role:     role,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(expirationTime),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString(currentJWTKey(h.cfg))
	if err != nil {
		InternalError(w, "Failed to generate token")
		return
	}

	Success(w, map[string]string{
		"token":    tokenString,
		"username": req.Username,
		"role":     role,
	})

	logActivity(h.activity, r, "auth.login", req.Username, map[string]interface{}{
		"ip": clientIP,
	})
}

// ChangePassword updates the user's password
func (h *AuthHandler) ChangePassword(w http.ResponseWriter, r *http.Request) {
	user, ok := GetUserFromContext(r.Context())
	if !ok || user.Username == "" {
		Unauthorized(w, "Missing authenticated user")
		return
	}

	var req ChangePasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		BadRequest(w, "Invalid request payload")
		return
	}
	if strings.TrimSpace(req.OldPassword) == "" || strings.TrimSpace(req.NewPassword) == "" {
		BadRequest(w, "Old and new password are required")
		return
	}

	storedUser, ok := h.cfg.Users[user.Username]
	if !ok || !config.CheckPassword(req.OldPassword, storedUser.Password) {
		Unauthorized(w, "Invalid old password")
		return
	}

	hashedPassword, err := config.HashPassword(req.NewPassword)
	if err != nil {
		BadRequest(w, err.Error())
		return
	}

	storedUser.Password = hashedPassword
	h.cfg.Users[user.Username] = storedUser
	if err := h.cfg.Save(); err != nil {
		log.Error().Err(err).Msg("Failed to save config after password change")
		InternalError(w, "Failed to save configuration")
		return
	}

	logActivity(h.activity, r, "auth.password_changed", user.Username, nil)
	MessageResponse(w, "Password changed successfully")
}

func (h *AuthHandler) checkRateLimit(key string) (bool, time.Duration) {
	now := time.Now()
	h.mu.Lock()
	defer h.mu.Unlock()

	state := h.rate[key]
	if state.windowStart.IsZero() || now.Sub(state.windowStart) > rateLimitWindow {
		state.windowStart = now
		state.count = 0
	}

	if state.count >= rateLimitMaxHits {
		retry := rateLimitWindow - now.Sub(state.windowStart)
		return true, retry
	}

	state.count++
	h.rate[key] = state
	return false, 0
}

func (h *AuthHandler) isLocked(username string) (bool, time.Duration) {
	if username == "" {
		return false, 0
	}
	now := time.Now()
	h.mu.Lock()
	defer h.mu.Unlock()

	state := h.lockouts[username]
	if state.lockedUntil.After(now) {
		return true, state.lockedUntil.Sub(now)
	}
	return false, 0
}

func (h *AuthHandler) registerFailure(username string) {
	if username == "" {
		return
	}
	now := time.Now()
	h.mu.Lock()
	defer h.mu.Unlock()

	state := h.lockouts[username]
	state.failed++
	if state.failed >= loginFailLimit {
		state.failed = 0
		state.lockedUntil = now.Add(lockoutDuration)
	}
	h.lockouts[username] = state
}

func (h *AuthHandler) resetFailures(username string) {
	if username == "" {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.lockouts, username)
}

func getClientIP(r *http.Request) string {
	if r == nil {
		return ""
	}
	if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
		parts := strings.Split(forwarded, ",")
		if len(parts) > 0 {
			return strings.TrimSpace(parts[0])
		}
	}
	if realIP := r.Header.Get("X-Real-IP"); realIP != "" {
		return realIP
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

// AuthMiddleware verifies JWT token
func AuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tokenStr := r.Header.Get("Authorization")
		if tokenStr == "" {
			Unauthorized(w, "Missing authorization header")
			return
		}

		if len(tokenStr) > 7 && tokenStr[:7] == "Bearer " {
			tokenStr = tokenStr[7:]
		}

		claims := &Claims{}
		token, err := jwt.ParseWithClaims(tokenStr, claims, func(token *jwt.Token) (interface{}, error) {
			return currentJWTKey(nil), nil
		})

		if err != nil || !token.Valid {
			Unauthorized(w, "Invalid token")
			return
		}

		ctx := context.WithValue(r.Context(), userContextKey, UserContext{
			Username: claims.Username,
			Role:     strings.ToLower(claims.Role),
		})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequireRoles ensures the user has one of the allowed roles
func RequireRoles(roles ...string) func(http.Handler) http.Handler {
	roleSet := make(map[string]struct{}, len(roles))
	for _, r := range roles {
		roleSet[strings.ToLower(r)] = struct{}{}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user, ok := r.Context().Value(userContextKey).(UserContext)
			if !ok || user.Role == "" {
				Forbidden(w, "Forbidden")
				return
			}
			if _, allowed := roleSet[user.Role]; !allowed {
				Forbidden(w, "Forbidden")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// SetupAuthRoutes adds auth routes
func (h *AuthHandler) SetupRoutes(r chi.Router) {
	r.Post("/auth/login", h.Login)
}

func (h *AuthHandler) SetupProtectedRoutes(r chi.Router) {
	r.Post("/auth/change-password", h.ChangePassword)
}

func currentJWTKey(cfg *config.Config) []byte {
	jwtKeyOnce.Do(func() {
		secret := strings.TrimSpace(os.Getenv("OLT_JWT_SECRET"))
		if secret == "" && cfg != nil {
			secret = strings.TrimSpace(cfg.Server.JWTSecret)
		}
		if secret == "" {
			secret = generateRandomSecret()
			log.Warn().Msg("JWT secret not configured; generated ephemeral secret for current process")
		}
		jwtKey = []byte(secret)
	})

	return jwtKey
}

func generateRandomSecret() string {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return time.Now().UTC().Format(time.RFC3339Nano)
	}

	return base64.RawStdEncoding.EncodeToString(buf)
}

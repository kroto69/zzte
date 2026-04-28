package handler

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/rs/zerolog/log"

	"olt-monitor/internal/cache"
	"olt-monitor/internal/config"
	"olt-monitor/internal/service"
)

// NewRouter creates and configures the Chi router
func NewRouter(oltHandler *OLTHandler, onuHandler *ONUHandler, authHandler *AuthHandler, userHandler *UserHandler, systemHandler *SystemHandler, searchHandler *SearchHandler, controlHandler *ControlHandler, provisioningHandler *ProvisioningHandler, activityHandler *ActivityHandler) *chi.Mux {
	r := chi.NewRouter()

	// Middleware
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(zerologMiddleware)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(60 * time.Second))

	// CORS
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-Request-ID"},
		ExposedHeaders:   []string{"Link"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	// Health check
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		Success(w, map[string]string{"status": "ok"})
	})

	// API v1 routes
	r.Route("/api/v1", func(r chi.Router) {
		authHandler.SetupRoutes(r)

		r.Group(func(r chi.Router) {

			authHandler.SetupProtectedRoutes(r)

			r.Group(func(r chi.Router) {


				// OLT routes (read)
				r.Get("/olts", oltHandler.ListOLTs)
				r.Get("/olt/{oltId}", oltHandler.GetOLT)

				// ONU routes
				r.Get("/olt/{oltId}/board/{board}/pon/{pon}", onuHandler.GetONUList)
				r.Get("/olt/{oltId}/board/{board}/pon/{pon}/onu/{onuId}", onuHandler.GetONUDetail)
				r.Get("/olt/{oltId}/board/{board}/pon", onuHandler.GetPONList)

				// Control routes
				r.Post("/onu/reboot", controlHandler.RebootONU)

				// System routes (for Dashboard)
				r.Get("/system/olts", systemHandler.GetAllSystemInfo)
				r.Get("/system/olt/{oltId}", systemHandler.GetSystemInfo)

				// Search routes (read)
				r.Get("/search", searchHandler.Search)
				r.Get("/search/stats", searchHandler.GetStats)
			})

			r.Group(func(r chi.Router) {


				// OLT routes (write)
				r.Post("/olt/test-connection", oltHandler.TestConnection)
				r.Post("/olt", oltHandler.CreateOLT)
				r.Put("/olt/{oltId}", oltHandler.UpdateOLT)
				r.Delete("/olt/{oltId}", oltHandler.DeleteOLT)

				// Search routes (admin)
				r.Post("/search/sync", searchHandler.ForceSync)
				r.Get("/search/config", searchHandler.GetConfig)
				r.Post("/search/config", searchHandler.UpdateConfig)

				// Provisioning routes
				r.Get("/provisioning/unconfigured", provisioningHandler.GetUnconfiguredONUs)
				r.Post("/provisioning/preview", provisioningHandler.PreviewProvisioning)
				r.Post("/provisioning/execute", provisioningHandler.ProvisionONU)

				// User management
				r.Get("/users", userHandler.ListUsers)
				r.Post("/users", userHandler.CreateUser)
				r.Put("/users/{username}", userHandler.UpdateUser)
				r.Delete("/users/{username}", userHandler.DeleteUser)

				// Activity logs
				r.Get("/activity", activityHandler.List)
			})
		})
	})

	return r
}

// zerologMiddleware logs HTTP requests using zerolog
func zerologMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)

		defer func() {
			log.Info().
				Str("method", r.Method).
				Str("path", r.URL.Path).
				Int("status", ww.Status()).
				Dur("latency", time.Since(start)).
				Str("remote", r.RemoteAddr).
				Msg("request")
		}()

		next.ServeHTTP(ww, r)
	})
}

// SetupRoutes creates all handlers and returns configured router
func SetupRoutes(cfg *config.Config, manager *service.OLTManager, onuService *service.ONUService, indexerService *service.IndexerService, cache *cache.RedisCache) *chi.Mux {
	telnetService := service.NewTelnetService()
	activityService := service.NewActivityService(cache)
	oltHandler := NewOLTHandler(manager, indexerService, cfg, telnetService, activityService)
	onuHandler := NewONUHandler(onuService)
	authHandler := NewAuthHandler(cfg, activityService)
	userHandler := NewUserHandler(cfg)
	searchHandler := NewSearchHandler(cache, indexerService)
	activityHandler := NewActivityHandler(activityService)

	systemService := service.NewSystemServiceWithCache(manager, cache)
	systemHandler := NewSystemHandler(systemService)

	controlHandler := NewControlHandler(manager, telnetService, activityService)

	provisioningService := service.NewProvisioningService(manager, telnetService)
	provisioningHandler := NewProvisioningHandler(provisioningService, manager, activityService)

	r := NewRouter(oltHandler, onuHandler, authHandler, userHandler, systemHandler, searchHandler, controlHandler, provisioningHandler, activityHandler)

	return r
}

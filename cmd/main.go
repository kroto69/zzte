package main

import (
	"context"
	"os"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"olt-monitor/internal/cache"
	"olt-monitor/internal/config"
	"olt-monitor/internal/handler"
	"olt-monitor/internal/server"
	"olt-monitor/internal/service"
)

func main() {
	// Setup logging
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})

	// Load configuration
	cfg, err := config.Load(".")
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to load configuration")
	}

	// Set log level
	level, err := zerolog.ParseLevel(cfg.Server.LogLevel)
	if err != nil {
		level = zerolog.InfoLevel
	}
	zerolog.SetGlobalLevel(level)

	// Setup Cache
	addr := cfg.Redis.GetRedisAddr()
	redisCache, err := cache.NewRedisCache(addr, cfg.Redis.Password, cfg.Redis.DB)
	if err != nil {
		log.Warn().Err(err).Msg("Failed to connect to Redis, caching disabled")
		redisCache = nil
	} else {
		defer redisCache.Close()
	}

	// Initialize services
	manager := service.InitOLTManager(redisCache)

	// Register OLTs from config
	for id, oltCfg := range cfg.OLTs {
		instance := oltCfg.ToOLTInstance(id)
		// Fix: Use context.Background() instead of nil
		if _, err := manager.RegisterOLT(context.Background(), instance); err != nil {
			log.Error().Err(err).Str("oltId", id).Msg("Failed to register OLT from config")
		} else {
			log.Info().Str("oltId", id).Msg("Registered OLT from config")
		}
	}

	// Create services
	onuService := service.NewONUService(manager, redisCache)
	indexerService := service.NewIndexerService(manager, redisCache, cfg)

	// Start Background Sync (every 10 minutes)
	indexerService.StartBackgroundSync(10 * time.Minute)

	// Setup HTTP server
	router := handler.SetupRoutes(cfg, manager, onuService, indexerService, redisCache)
	srv := server.NewServer(cfg, router)

	// Start server
	log.Info().Int("port", cfg.Server.Port).Msg("Starting server")
	if err := srv.Start(); err != nil {
		log.Fatal().Err(err).Msg("Server failed")
	}
}

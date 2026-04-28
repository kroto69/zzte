package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"olt-monitor/internal/cache"
	"olt-monitor/internal/config"
	"olt-monitor/internal/handler"
	"olt-monitor/internal/poller"
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
		if _, err := manager.RegisterOLT(context.Background(), instance); err != nil {
			log.Error().Err(err).Str("oltId", id).Msg("Failed to register OLT from config")
		} else {
			log.Info().Str("oltId", id).Msg("Registered OLT from config")
		}
	}

	// Create services
	onuService := service.NewONUService(manager, redisCache)
	indexerService := service.NewIndexerService(manager, redisCache, cfg)

	// Context untuk graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start optical poller jika enabled
	var opticalPoller *poller.OpticalPoller
	if cfg.OpticalPoller.Enabled {
		opticalPoller = poller.NewOpticalPoller(manager, redisCache, cfg)
		opticalPoller.Start(ctx)
		log.Info().Int("interval", cfg.OpticalPoller.Interval).Msg("Optical poller started")
	}

	// Start names poller jika enabled
	var namesPoller *poller.NamesPoller
	if cfg.NamesPoller.Enabled {
		namesPoller = poller.NewNamesPoller(manager, redisCache, cfg)
		namesPoller.Start(ctx)
		log.Info().Int("interval", cfg.NamesPoller.Interval).Msg("Names poller started")
	}

	// Start background search indexer jika enabled
	if cfg.Search.Enabled && cfg.Search.Interval > 0 {
		indexerService.StartBackgroundSync(time.Duration(cfg.Search.Interval) * time.Minute)
	}

	// Setup HTTP server
	router := handler.SetupRoutes(cfg, manager, onuService, indexerService, redisCache)
	srv := server.NewServer(cfg, router)

	// Graceful shutdown
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		sig := <-sigCh
		log.Info().Str("signal", sig.String()).Msg("Menerima signal shutdown")

		cancel() // Batalkan context poller

		if opticalPoller != nil {
			opticalPoller.Stop()
		}
		if namesPoller != nil {
			namesPoller.Stop()
		}

		log.Info().Msg("Shutdown selesai")
		os.Exit(0)
	}()

	// Start server
	log.Info().Int("port", cfg.Server.Port).Msg("Starting server")
	if err := srv.Start(); err != nil {
		log.Fatal().Err(err).Msg("Server failed")
	}
}
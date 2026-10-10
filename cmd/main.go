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
	"olt-monitor/internal/event"
	"olt-monitor/internal/handler"
	"olt-monitor/internal/poller"
	"olt-monitor/internal/server"
	"olt-monitor/internal/service"
	"olt-monitor/internal/snmp"
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
	// PATCH(oltwtch-2): tracker port panas — dipakai service (menandai) dan
	// poller (memprioritaskan). Lihat internal/service/hot_tracker.go.
	hotTracker := service.NewHotTracker()
	onuService := service.NewONUService(manager, redisCache, hotTracker)
	indexerService := service.NewIndexerService(manager, redisCache, cfg)

	// Context untuk graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start optical poller jika enabled
	var opticalPoller *poller.OpticalPoller
	if cfg.OpticalPoller.Enabled {
		opticalPoller = poller.NewOpticalPoller(manager, redisCache, cfg, hotTracker)
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

	// PATCH(oltwtch-event): jalur event-driven via syslog dari OLT.
	// PRINSIP: OLT hanya didengarkan, tidak ditanya — nol beban polling/status.
	// Multi-OLT: IP pengirim di-resolve ke oltID (config eksplisit atau
	// fallback ke host SNMP OLT terdaftar).
	eventBus := event.NewBus(1024)
	eventBus.Start()
	defer eventBus.Close()

	eventMapper := event.NewMapper(manager)
	eventMapper.SetSources(cfg.Event.Syslog.Sources)

	// Sink Redis: patch status instan + pub/sub realtime + event log.
	eventSink := event.NewSink(redisCache)
	eventBus.Subscribe(eventSink.Handle)

	// PATCH(oltwtch-alert): mesin alert — event down yang lolos anti-spam
	// (agregasi + cooldown) disimpan di Redis dan diekspos via
	// GET /api/v1/alerts untuk ditampilkan frontend. Tanpa WA.
	alertEngine := event.NewEngine(event.AlertConfig{
		Cooldown:      time.Duration(cfg.Event.Alert.CooldownMinutes) * time.Minute,
		AggWindow:     time.Duration(cfg.Event.Alert.AggWindowSeconds) * time.Second,
		MassThreshold: cfg.Event.Alert.MassThreshold,
	}, redisCache)
	if cfg.Event.Alert.Reverify {
		// PATCH(oltwtch-alert): verifikasi ulang status ONU tepat sebelum
		// alert diterbitkan — satu SNMP GET terarah, bukan polling.
		// Kalau ONU sudah online lagi (status 3), alert dibuang: ini
		// menutup false-alarm klasik saat syslog LOS tapi ONU sudah
		// kembali up. Kesalahan SNMP -> trust syslog (alert tetap keluar).
		alertEngine.SetVerifier(func(ev event.Event) bool {
			cl, err := manager.GetNewClient(ev.OLTID)
			if err != nil {
				return true
			}
			if err := cl.Connect(); err != nil {
				return true
			}
			defer cl.Close()
			ifIndex := snmp.CalculateIfIndexDefault(ev.Board, ev.Pon)
			oid := snmp.BuildONUOID(snmp.OIDONUStatus, ifIndex, ev.OnuID, "")
			st, err := cl.GetInt(oid)
			if err != nil {
				return true
			}
			return st != 3 // 3 = online
		})
	}
	eventBus.Subscribe(alertEngine.Handle)
	alertEngine.Start()
	defer alertEngine.Stop()
	log.Info().Msg("Alert engine aktif (anti-spam: agregasi + cooldown, via /api/v1/alerts)")

	var syslogStop chan struct{}
	if cfg.Event.Syslog.Enabled {
		syslogStop = make(chan struct{})
		syslogSrv := event.NewServer(eventBus, eventMapper, cfg.Event.Syslog.Port)
		go func() {
			if err := syslogSrv.Serve(syslogStop); err != nil {
				log.Error().Err(err).Msg("Syslog server berhenti dengan error")
			}
		}()
		log.Info().Int("port", cfg.Event.Syslog.Port).
			Msg("Syslog listener aktif — arahkan syslog OLT ke IP:port ini")
	} else {
		log.Info().Msg("Event syslog nonaktif (event.syslog.enabled=false)")
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

		if syslogStop != nil {
			close(syslogStop)
		}
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
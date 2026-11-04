package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"spidi-agent/internal/config"
	"spidi-agent/internal/database"
	"spidi-agent/internal/handlers"
	"spidi-agent/internal/logger"
	"spidi-agent/internal/middleware"
	"spidi-agent/internal/service"
	"spidi-agent/internal/spidi"
	"spidi-agent/internal/tenant"
)

func main() {
	cfgPath := ""
	if len(os.Args) > 1 {
		cfgPath = os.Args[1]
	}

	cfg, err := config.Load(cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "config error: %v\n", err)
		os.Exit(1)
	}

	log := logger.New(cfg.Logging.Level, cfg.Logging.Format == "json")

	if err := cfg.Validate(); err != nil {
		log.Error("config validation failed", "error", err)
		os.Exit(1)
	}

	db, err := database.Open(database.Config{
		Path:     cfg.Database.Path,
		Log:      log,
		PoolSize: cfg.Database.PoolSize,
	})
	if err != nil {
		log.Error("database open failed", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	tenantStore := tenant.NewStore(db, cfg.Security.MasterKey, log)
	spidiClient := spidi.NewClient("sandbox", log)

	svc := service.New(service.Config{
		DB:          db,
		TenantStore: tenantStore,
		SpidiClient: spidiClient,
		Logger:      log,
	})

	handler := handlers.NewHandler(svc, log)

	// Registrar tenants de prueba (después iría desde DB o API)
	_ = middleware.NewTenantAuther(log)

	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	// Encadenar middlewares
	var h http.Handler = mux
	h = middleware.LoggingMiddleware(log)(h)
	// h = tenantAuth.Middleware(h)  // después

	server := &http.Server{
		Addr:         cfg.BindAddr(),
		Handler:      h,
		ReadTimeout:  time.Duration(cfg.Server.ReadTimeout) * time.Second,
		WriteTimeout: time.Duration(cfg.Server.WriteTimeout) * time.Second,
	}

	// Graceful shutdown
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		log.Info("shutting down gracefully...")
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		server.Shutdown(ctx)
		db.Close()
	}()

	log.Info("spidi agent starting", "addr", cfg.BindAddr())
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Error("server failed", "error", err)
		os.Exit(1)
	}
}

package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "github.com/lib/pq"

	"github.com/wso2/asdlc/database-service/api"
	"github.com/wso2/asdlc/database-service/config"
	"github.com/wso2/asdlc/database-service/controllers"
	"github.com/wso2/asdlc/database-service/database"
	"github.com/wso2/asdlc/database-service/repository"
	"github.com/wso2/asdlc/database-service/services"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to load config: %v\n", err)
		os.Exit(1)
	}

	setupLogger(cfg.LogLevel)

	// Internal PostgreSQL — mapping store for (org, project, component) → database.
	db, err := database.Open(cfg.DatabaseURL)
	if err != nil {
		slog.Error("failed to open internal database", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	mappingRepo := repository.NewMappingRepository(db)

	dbService := services.NewDatabaseService(
		cfg.MySQLRootURL, cfg.MySQLHost, cfg.MySQLPort,
		cfg.MongoRootURL, cfg.MongoHost, cfg.MongoPort,
		mappingRepo,
	)

	dbCtrl := controllers.NewDatabaseController(dbService)

	handler := api.NewHandler(api.AppParams{
		DatabaseCtrl: dbCtrl,
		DatabaseSvc:  dbService,
	})

	server := &http.Server{
		Addr:         fmt.Sprintf("%s:%d", cfg.ServerHost, cfg.ServerPort),
		Handler:      handler,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 5 * time.Minute,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		slog.Info("server started", "addr", server.Addr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	slog.Info("shutting down server")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		slog.Error("server shutdown failed", "error", err)
		os.Exit(1)
	}
	slog.Info("server stopped")
}

func setupLogger(level string) {
	var logLevel slog.Level
	switch level {
	case "debug":
		logLevel = slog.LevelDebug
	case "warn":
		logLevel = slog.LevelWarn
	case "error":
		logLevel = slog.LevelError
	default:
		logLevel = slog.LevelInfo
	}
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: logLevel,
	})))
}

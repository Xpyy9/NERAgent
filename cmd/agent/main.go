package main

import (
	"NERAgent/internal/agent"
	"NERAgent/internal/config"
	"NERAgent/web"
	"context"
	"net/http"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		panic(err)
	}

	if err := agent.Init(cfg); err != nil {
		panic(err)
	}

	addr := cfg.Web.ListenAddr()
	server := &http.Server{
		Addr:    addr,
		Handler: web.SetRouter(cfg),
	}
	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			panic(err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	_ = server.Shutdown(context.Background())
}

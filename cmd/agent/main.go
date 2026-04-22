package main

import (
	"NERAgent/internal/agent"
	"NERAgent/web"
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/joho/godotenv"
)

func main() {
	// 加载 .env（agent.Init 内部也会调用，此处提前加载以读取 WEB_HOST/WEB_PORT）
	_ = godotenv.Load()

	// Agent 初始化
	if err := agent.Init(); err != nil {
		log.Fatalf("[+]Main===Agent Init Failed: %v", err)
	}
	log.Printf("[+]===NERAgent Init===")

	// HTTP 服务
	addr := web.ListenAddr()
	server := &http.Server{
		Addr:    addr,
		Handler: web.SetRouter(),
	}
	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("HTTP server error: %v", err)
		}
	}()
	log.Printf("[+]===NERAgent Server Listening on %s===", addr)

	// 优雅关闭
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	sig := <-quit
	log.Printf("[+]Received signal: %v, shutting down...", sig)

	if err := server.Shutdown(context.Background()); err != nil {
		log.Printf("[-]Server shutdown error: %v", err)
	}
}

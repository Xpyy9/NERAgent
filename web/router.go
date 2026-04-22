package web

import (
	"fmt"
	"log"
	"os"

	"github.com/gin-gonic/gin"
)

// SetRouter 注册接口路由并返回 Engine
func SetRouter() *gin.Engine {
	r := gin.Default()

	r.POST("/chat", ChatHandler)
	r.POST("/cancel", CancelHandler)
	r.GET("/api/jadx-status", JadxStatusHandler)
	r.GET("/api/models", ModelsGetHandler)
	r.PUT("/api/models", ModelsPutHandler)

	r.Static("/static", "./src/html/static")
	r.GET("/", func(c *gin.Context) {
		c.File("./src/html/index.html")
	})

	log.Printf("[+]===Router Setting Done===")
	return r
}

// ListenAddr 从环境变量获取监听地址，默认 127.0.0.1:13998
func ListenAddr() string {
	host := os.Getenv("WEB_HOST")
	if host == "" {
		host = "127.0.0.1"
	}
	port := os.Getenv("WEB_PORT")
	if port == "" {
		port = "13998"
	}
	return fmt.Sprintf("%s:%s", host, port)
}

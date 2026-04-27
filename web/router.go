package web

import (
	"NERAgent/internal/config"

	"github.com/gin-gonic/gin"
)

// SetRouter registers routes and returns the Engine.
func SetRouter(cfg *config.Config) *gin.Engine {
	jadxBaseURL = cfg.JADX.BaseURL

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

	return r
}

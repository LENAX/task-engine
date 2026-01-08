package api

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/stevelan1995/task-engine/pkg/core/engine"
)

// ServerConfig API服务器配置
type ServerConfig struct {
	Host         string        // 监听地址
	Port         int           // 监听端口
	ReadTimeout  time.Duration // 读取超时
	WriteTimeout time.Duration // 写入超时
}

// DefaultServerConfig 默认服务器配置
func DefaultServerConfig() ServerConfig {
	return ServerConfig{
		Host:         "0.0.0.0",
		Port:         8080,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
	}
}

// APIServer HTTP API服务器
type APIServer struct {
	engine     *engine.Engine
	httpServer *http.Server
	config     ServerConfig
	version    string
}

// NewAPIServer 创建API服务器
func NewAPIServer(eng *engine.Engine, config ServerConfig, version string) *APIServer {
	return &APIServer{
		engine:  eng,
		config:  config,
		version: version,
	}
}

// Start 启动服务器
func (s *APIServer) Start() error {
	router := SetupRouter(s.engine, s.version)

	addr := fmt.Sprintf("%s:%d", s.config.Host, s.config.Port)

	s.httpServer = &http.Server{
		Addr:         addr,
		Handler:      router,
		ReadTimeout:  s.config.ReadTimeout,
		WriteTimeout: s.config.WriteTimeout,
	}

	log.Printf("🚀 Task Engine API Server starting on %s", addr)

	if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("server listen failed: %w", err)
	}

	return nil
}

// Shutdown 优雅关闭服务器
func (s *APIServer) Shutdown(ctx context.Context) error {
	if s.httpServer == nil {
		return nil
	}

	log.Println("🛑 Shutting down API Server...")

	if err := s.httpServer.Shutdown(ctx); err != nil {
		return fmt.Errorf("server shutdown failed: %w", err)
	}

	log.Println("✅ API Server stopped")
	return nil
}

// Addr 获取服务器地址
func (s *APIServer) Addr() string {
	return fmt.Sprintf("%s:%d", s.config.Host, s.config.Port)
}

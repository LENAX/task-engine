package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/LENAX/task-engine/pkg/api"
	"github.com/LENAX/task-engine/pkg/core/engine"
)

var (
	Version   = "0.1.0"
	GitCommit = "unknown"
	BuildTime = "unknown"
)

func main() {
	// 命令行参数
	configPath := flag.String("config", "./configs/engine.yaml", "引擎配置文件路径")
	host := flag.String("host", "0.0.0.0", "监听地址")
	port := flag.Int("port", 8080, "监听端口")
	flag.Parse()

	log.Printf("Task Engine Server v%s", Version)
	log.Printf("配置文件: %s", *configPath)

	// 1. 构建Engine
	eng, err := engine.NewEngineBuilder(*configPath).
		RestoreFunctionsOnStart().
		Build()
	if err != nil {
		log.Fatalf("创建Engine失败: %v", err)
	}

	// 2. 启动Engine
	ctx := context.Background()
	if err := eng.Start(ctx); err != nil {
		log.Fatalf("启动Engine失败: %v", err)
	}

	// 3. 创建API服务器
	config := api.ServerConfig{
		Host:         *host,
		Port:         *port,
		ReadTimeout:  api.DefaultServerConfig().ReadTimeout,
		WriteTimeout: api.DefaultServerConfig().WriteTimeout,
	}

	apiServer := api.NewAPIServer(eng, config, Version)

	// 4. 在goroutine中启动API服务器
	go func() {
		if err := apiServer.Start(); err != nil {
			log.Printf("API服务器错误: %v", err)
		}
	}()

	log.Printf("✅ Task Engine Server started on %s:%d", *host, *port)

	// 5. 等待中断信号
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("正在关闭服务...")

	// 6. 优雅关闭
	shutdownCtx, cancel := context.WithTimeout(context.Background(), api.DefaultServerConfig().WriteTimeout)
	defer cancel()

	if err := apiServer.Shutdown(shutdownCtx); err != nil {
		log.Printf("关闭API服务器失败: %v", err)
	}

	eng.Stop()
	log.Println("✅ 服务已停止")
}

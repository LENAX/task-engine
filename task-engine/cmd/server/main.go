package main

import (
    "context"
    "fmt"
    "log"
    "net/http"
    "os"
    "os/signal"
    "syscall"
    "time"

    "github.com/stevelan1995/task-engine/api/router"
    "github.com/stevelan1995/task-engine/pkg/config"
    "github.com/stevelan1995/task-engine/pkg/engine"
)

// 程序入口：初始化引擎 + 启动HTTP服务
func main() {
    // 1. 初始化配置
    cfg, err := config.Load("configs/engine.yaml")
    if err != nil {
        log.Fatalf("❌ 加载配置失败: %v", err)
    }
    log.Printf("✅ 配置加载成功 | 引擎模式: %s", cfg.Mode)

    // 2. 初始化调度引擎
    eng, err := engine.NewEngine(cfg)
    if err != nil {
        log.Fatalf("❌ 引擎初始化失败: %v", err)
    }
    log.Println("✅ 量化任务引擎初始化完成")

    // 3. 初始化HTTP路由
    r := router.InitRouter(eng)

    // 4. 启动HTTP服务
    server := &http.Server{
        Addr:    fmt.Sprintf(":%d", cfg.HTTPPort),
        Handler: r,
    }

    // 异步启动服务
    go func() {
        log.Printf("✅ HTTP服务启动成功 | 地址: http://localhost:%d", cfg.HTTPPort)
        if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
            log.Fatalf("❌ HTTP服务启动失败: %v", err)
        }
    }()

    // 5. 优雅关闭
    quit := make(chan os.Signal, 1)
    signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
    <-quit
    log.Println("\n🔴 开始优雅关闭服务...")

    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()
    if err := server.Shutdown(ctx); err != nil {
        log.Fatalf("❌ 服务关闭失败: %v", err)
    }

    // 关闭引擎
    eng.Stop()
    log.Println("✅ 量化任务引擎已优雅关闭")
}

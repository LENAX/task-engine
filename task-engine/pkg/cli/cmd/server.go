package cmd

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/stevelan1995/task-engine/pkg/api"
	"github.com/stevelan1995/task-engine/pkg/cli/output"
	"github.com/stevelan1995/task-engine/pkg/core/engine"
)

var (
	serverPort   int
	configPath   string
	serverHost   string
)

// serverCmd server子命令
var serverCmd = &cobra.Command{
	Use:   "server",
	Short: "服务管理命令",
	Long:  `管理Task Engine HTTP API服务。`,
}

// serverStartCmd 启动服务
var serverStartCmd = &cobra.Command{
	Use:   "start",
	Short: "启动HTTP API服务",
	Long: `启动Task Engine HTTP API服务。

示例：
  # 使用默认配置启动
  task-engine server start

  # 指定端口启动
  task-engine server start --port 8080

  # 指定配置文件启动
  task-engine server start --config ./configs/engine.yaml`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// 检查配置文件
		if configPath == "" {
			// 尝试默认配置路径
			defaultPaths := []string{
				"./configs/engine.yaml",
				"./config/engine.yaml",
				"./engine.yaml",
			}
			for _, p := range defaultPaths {
				if _, err := os.Stat(p); err == nil {
					configPath = p
					break
				}
			}
			if configPath == "" {
				output.Error("未找到配置文件，请使用 --config 指定")
				return fmt.Errorf("config file not found")
			}
		}

		output.Info("使用配置文件: %s", configPath)

		// 创建Engine
		eng, err := engine.NewEngineBuilder(configPath).
			RestoreFunctionsOnStart().
			Build()
		if err != nil {
			output.Error("创建Engine失败: %v", err)
			return err
		}

		// 启动Engine
		ctx := context.Background()
		if err := eng.Start(ctx); err != nil {
			output.Error("启动Engine失败: %v", err)
			return err
		}

		// 创建API服务器配置
		config := api.ServerConfig{
			Host:         serverHost,
			Port:         serverPort,
			ReadTimeout:  api.DefaultServerConfig().ReadTimeout,
			WriteTimeout: api.DefaultServerConfig().WriteTimeout,
		}

		// 创建并启动API服务器
		apiServer := api.NewAPIServer(eng, config, Version)

		// 在goroutine中启动服务器
		go func() {
			if err := apiServer.Start(); err != nil {
				log.Printf("API服务器错误: %v", err)
			}
		}()

		output.Success("Task Engine Server started on %s:%d", serverHost, serverPort)

		// 等待中断信号
		quit := make(chan os.Signal, 1)
		signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
		<-quit

		output.Info("正在关闭服务...")

		// 优雅关闭
		shutdownCtx, cancel := context.WithTimeout(context.Background(), api.DefaultServerConfig().WriteTimeout)
		defer cancel()

		if err := apiServer.Shutdown(shutdownCtx); err != nil {
			output.Error("关闭API服务器失败: %v", err)
		}

		eng.Stop()
		output.Success("服务已停止")

		return nil
	},
}

func init() {
	serverStartCmd.Flags().IntVarP(&serverPort, "port", "p", 8080, "监听端口")
	serverStartCmd.Flags().StringVarP(&serverHost, "host", "H", "0.0.0.0", "监听地址")
	serverStartCmd.Flags().StringVarP(&configPath, "config", "c", "", "配置文件路径")

	serverCmd.AddCommand(serverStartCmd)
}

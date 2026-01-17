package cmd

import (
	"os"

	"github.com/spf13/cobra"
)

var (
	// 全局变量
	serverURL  string
	outputJSON bool
)

// rootCmd 根命令
var rootCmd = &cobra.Command{
	Use:   "task-engine",
	Short: "Task Engine CLI - 工作流引擎命令行工具",
	Long: `Task Engine CLI 是一个用于管理工作流的命令行工具。

支持的功能：
  - 管理Workflow（上传、列出、查看、删除、执行）
  - 管理Instance（列出、查看状态、暂停、恢复、取消）
  - 查询执行历史
  - 启动HTTP API服务

使用示例：
  # 列出所有Workflow
  task-engine workflow list

  # 执行Workflow
  task-engine workflow execute <workflow-id>

  # 查看Instance状态
  task-engine instance status <instance-id>

  # 启动HTTP服务
  task-engine server start --port 8080`,
}

// Execute 执行根命令
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func init() {
	// 全局参数
	rootCmd.PersistentFlags().StringVarP(&serverURL, "server", "s", "http://localhost:8080", "Task Engine服务器地址")
	rootCmd.PersistentFlags().BoolVarP(&outputJSON, "json", "j", false, "使用JSON格式输出")

	// 添加子命令
	rootCmd.AddCommand(workflowCmd)
	rootCmd.AddCommand(instanceCmd)
	rootCmd.AddCommand(serverCmd)
	rootCmd.AddCommand(versionCmd)
}

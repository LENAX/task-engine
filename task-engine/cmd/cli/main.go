package main

import (
	"fmt"
	"os"
)

// CLI工具入口：支持手动触发/查询Workflow
func main() {
	if len(os.Args) < 2 {
		fmt.Println("使用说明:")
		fmt.Println("  cli run <workflow-id>   - 运行指定Workflow实例")
		fmt.Println("  cli query <instance-id> - 查询实例状态")
		fmt.Println("  cli stop <instance-id>  - 停止实例")
		os.Exit(1)
	}

	// 初始化配置和引擎
	// cfg, _ := config.Load("configs/engine.yaml")
	// eng, _ := engine.NewEngine(cfg)

	// 解析命令
	cmd := os.Args[1]
	switch cmd {
	case "run":
		if len(os.Args) < 3 {
			fmt.Println("请指定Workflow ID")
			os.Exit(1)
		}
		fmt.Printf("开始运行Workflow: %s\n", os.Args[2])
		// TODO: 实现运行逻辑
	case "query":
		if len(os.Args) < 3 {
			fmt.Println("请指定实例ID")
			os.Exit(1)
		}
		fmt.Printf("查询实例状态: %s\n", os.Args[2])
		// TODO: 实现查询逻辑
	case "stop":
		if len(os.Args) < 3 {
			fmt.Println("请指定实例ID")
			os.Exit(1)
		}
		fmt.Printf("停止实例: %s\n", os.Args[2])
		// TODO: 实现停止逻辑
	default:
		fmt.Printf("未知命令: %s\n", cmd)
		os.Exit(1)
	}
}

package main

import (
	"context"
	"fmt"
	"log"
	"os"
)

// loadAndRunV2 使用新API加载任务并运行的脚本
func loadAndRunV2() {
	// 数据库路径
	dbPath := "tasks.db"
	if len(os.Args) > 2 {
		dbPath = os.Args[2]
	}

	// 检查数据库文件是否存在
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		log.Fatalf("数据库文件不存在: %s，请先运行保存任务", dbPath)
	}

	// 创建存储
	storage, err := NewStorage(dbPath)
	if err != nil {
		log.Fatalf("创建存储失败: %v", err)
	}
	defer storage.Close()

	// 创建注册中心
	registry := NewFunctionRegistry(storage)
	ctx := context.Background()

	fmt.Println("=== 使用新API加载任务和函数 ===")

	// 1. 恢复函数（从数据库加载元数据并注册函数实例）
	// 在实际使用中，用户需要提供函数映射表
	funcMap := map[string]interface{}{
		"AddNumbers":         AddNumbers,
		"GreetUser":          GreetUser,
		"ProcessData":        ProcessData,
		"DownloadFile":       DownloadFile,
		"ProcessOrder":       ProcessOrder,
		"ProcessUserList":    ProcessUserList,
		"ProcessConfig":      ProcessConfig,
		"ProcessUserProfile": ProcessUserProfile,
	}

	if err := registry.RestoreFunctions(ctx, funcMap); err != nil {
		log.Fatalf("恢复函数失败: %v", err)
	}
	fmt.Printf("✓ 恢复函数完成\n")

	// 2. 从数据库加载所有任务
	tasks, err := storage.ListAllTasks(ctx)
	if err != nil {
		log.Fatalf("加载任务失败: %v", err)
	}
	fmt.Printf("✓ 从数据库加载了 %d 个任务\n", len(tasks))

	// 3. 执行所有任务
	fmt.Println("\n=== 开始执行任务 ===")
	for i, task := range tasks {
		fmt.Printf("\n[任务 %d] %s (ID: %s)\n", i+1, task.Name, task.ID)
		fmt.Printf("  描述: %s\n", task.Description)
		fmt.Printf("  函数ID: %s\n", task.FuncID)

		// 获取函数
		fn := registry.Get(task.FuncID)
		if fn == nil {
			fmt.Printf("  ❌ 错误: 函数ID %s 未找到或未加载\n", task.FuncID)
			continue
		}

		// 获取函数元数据
		meta := registry.GetMeta(task.FuncID)
		if meta != nil {
			fmt.Printf("  函数名称: %s\n", meta.Name)
		}

		// 执行任务（通过channel获取结果）
		fmt.Printf("  参数: %v\n", task.Params)
		fmt.Printf("  开始执行...\n")

		stateCh := fn(ctx, task.Params)
		state, ok := <-stateCh
		if !ok {
			fmt.Printf("  ⚠ 警告: channel已关闭，未收到执行结果\n")
			continue
		}

		if state.Status == "Failed" {
			fmt.Printf("  ❌ 执行失败: %v\n", state.Error)
		} else {
			fmt.Printf("  ✓ 执行成功，状态: %s，结果: %v\n", state.Status, state.Data)
		}
	}

	fmt.Println("\n=== 执行完成 ===")
}

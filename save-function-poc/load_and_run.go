package main

import (
	"context"
	"fmt"
	"log"
	"os"
)

// loadAndRun 加载任务并运行的脚本
func loadAndRun() {
	// 数据库路径
	dbPath := "tasks.db"
	if len(os.Args) > 2 {
		dbPath = os.Args[2]
	}

	// 检查数据库文件是否存在
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		log.Fatalf("数据库文件不存在: %s，请先运行 save_task.go 保存任务", dbPath)
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

	fmt.Println("=== 开始加载任务和函数 ===")

	// 从数据库加载函数元数据
	metas, err := registry.LoadFromStorage(ctx)
	if err != nil {
		log.Fatalf("加载函数元数据失败: %v", err)
	}
	fmt.Printf("✓ 从数据库加载了 %d 个函数元数据\n", len(metas))

	// 根据函数名称重新注册函数实例
	// 注意：在实际场景中，函数实例需要从代码中获取
	// 这里我们使用函数名称映射到实际的函数
	funcMap := map[string]interface{}{
		"AddNumbers":   AddNumbers,
		"GreetUser":    GreetUser,
		"ProcessData":  ProcessData,
		"DownloadFile": DownloadFile,
	}

	for _, meta := range metas {
		fn, exists := funcMap[meta.Name]
		if !exists {
			fmt.Printf("⚠ 警告: 函数 %s (ID: %s) 的实例未找到，跳过\n", meta.Name, meta.ID)
			continue
		}

		if err := registry.LoadFunction(ctx, meta.ID, fn); err != nil {
			fmt.Printf("⚠ 警告: 加载函数 %s 失败: %v\n", meta.Name, err)
			continue
		}
		fmt.Printf("✓ 加载函数实例: %s (ID: %s)\n", meta.Name, meta.ID)
	}

	// 从数据库加载所有任务
	tasks, err := storage.ListAllTasks(ctx)
	if err != nil {
		log.Fatalf("加载任务失败: %v", err)
	}
	fmt.Printf("\n✓ 从数据库加载了 %d 个任务\n", len(tasks))

	// 执行每个任务
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

		// 从channel获取执行状态
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

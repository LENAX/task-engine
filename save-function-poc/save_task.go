package main

import (
	"context"
	"fmt"
	"log"
	"os"
)

// saveTask 保存任务和函数的脚本
func saveTask() {
	// 数据库路径
	dbPath := "tasks.db"
	if len(os.Args) > 2 {
		dbPath = os.Args[2]
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

	fmt.Println("=== 开始注册函数和保存任务 ===")

	// 注册函数1: AddNumbers
	funcID1, err := registry.Register(ctx, "AddNumbers", AddNumbers, "两个数字相加")
	if err != nil {
		log.Fatalf("注册函数AddNumbers失败: %v", err)
	}
	fmt.Printf("✓ 注册函数 AddNumbers, ID: %s\n", funcID1)

	// 注册函数2: GreetUser
	funcID2, err := registry.Register(ctx, "GreetUser", GreetUser, "问候用户")
	if err != nil {
		log.Fatalf("注册函数GreetUser失败: %v", err)
	}
	fmt.Printf("✓ 注册函数 GreetUser, ID: %s\n", funcID2)

	// 注册函数3: ProcessData
	funcID3, err := registry.Register(ctx, "ProcessData", ProcessData, "处理数据")
	if err != nil {
		log.Fatalf("注册函数ProcessData失败: %v", err)
	}
	fmt.Printf("✓ 注册函数 ProcessData, ID: %s\n", funcID3)

	// 注册函数4: DownloadFile
	funcID4, err := registry.Register(ctx, "DownloadFile", DownloadFile, "下载文件")
	if err != nil {
		log.Fatalf("注册函数DownloadFile失败: %v", err)
	}
	fmt.Printf("✓ 注册函数 DownloadFile, ID: %s\n", funcID4)

	// 创建任务1: 计算任务
	task1 := NewTask("计算任务", "计算两个数字的和", funcID1)
	task1.Params["arg0"] = "10"
	task1.Params["arg1"] = "20"
	if err := storage.SaveTask(ctx, task1); err != nil {
		log.Fatalf("保存任务1失败: %v", err)
	}
	fmt.Printf("✓ 保存任务: %s (ID: %s)\n", task1.Name, task1.ID)

	// 创建任务2: 问候任务
	task2 := NewTask("问候任务", "问候用户", funcID2)
	task2.Params["arg0"] = "Alice"
	if err := storage.SaveTask(ctx, task2); err != nil {
		log.Fatalf("保存任务2失败: %v", err)
	}
	fmt.Printf("✓ 保存任务: %s (ID: %s)\n", task2.Name, task2.ID)

	// 创建任务3: 数据处理任务
	task3 := NewTask("数据处理任务", "处理数据", funcID3)
	task3.Params["arg0"] = "test data"
	task3.Params["arg1"] = "5"
	if err := storage.SaveTask(ctx, task3); err != nil {
		log.Fatalf("保存任务3失败: %v", err)
	}
	fmt.Printf("✓ 保存任务: %s (ID: %s)\n", task3.Name, task3.ID)

	// 创建任务4: 文件下载任务
	task4 := NewTask("文件下载任务", "下载文件", funcID4)
	task4.Params["arg0"] = "https://example.com/file.zip"
	task4.Params["arg1"] = "3"
	if err := storage.SaveTask(ctx, task4); err != nil {
		log.Fatalf("保存任务4失败: %v", err)
	}
	fmt.Printf("✓ 保存任务: %s (ID: %s)\n", task4.Name, task4.ID)

	fmt.Println("\n=== 保存完成 ===")
	fmt.Printf("数据库文件: %s\n", dbPath)
	fmt.Printf("已注册函数数: %d\n", len(registry.metaMap))
}

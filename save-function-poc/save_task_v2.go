package main

import (
	"context"
	"fmt"
	"log"
	"os"
)

// saveTaskV2 使用新API保存任务和函数的脚本
func saveTaskV2() {
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

	fmt.Println("=== 使用新API注册函数和保存任务 ===")

	// 方式1: 批量注册函数
	functions := []FunctionDef{
		{Name: "AddNumbers", Description: "两个数字相加", Function: AddNumbers},
		{Name: "GreetUser", Description: "问候用户", Function: GreetUser},
		{Name: "ProcessData", Description: "处理数据", Function: ProcessData},
		{Name: "DownloadFile", Description: "下载文件", Function: DownloadFile},
		// 自定义类型测试函数
		{Name: "ProcessOrder", Description: "处理订单（自定义类型）", Function: ProcessOrder},
		{Name: "ProcessUserList", Description: "处理用户列表（slice）", Function: ProcessUserList},
		{Name: "ProcessConfig", Description: "处理配置（map）", Function: ProcessConfig},
		{Name: "ProcessUserProfile", Description: "处理用户资料（嵌套结构体）", Function: ProcessUserProfile},
	}

	if err := registry.RegisterBatch(ctx, functions); err != nil {
		log.Fatalf("批量注册函数失败: %v", err)
	}
	fmt.Printf("✓ 批量注册了 %d 个函数\n", len(functions))

	// 方式2: 通过函数引用创建任务（自动注册）
	task1, err := NewTaskWithFunction(ctx, registry,
		"计算任务",
		"计算两个数字的和",
		AddNumbers,
		"AddNumbers",
		"两个数字相加",
	)
	if err != nil {
		log.Fatalf("创建任务失败: %v", err)
	}
	task1.Params["arg0"] = "10"
	task1.Params["arg1"] = "20"
	if err := storage.SaveTask(ctx, task1); err != nil {
		log.Fatalf("保存任务失败: %v", err)
	}
	fmt.Printf("✓ 保存任务: %s (通过函数引用创建)\n", task1.Name)

	// 方式3: 通过函数ID创建任务
	funcID := registry.GetIDByName("GreetUser")
	task2 := NewTask("问候任务", "问候用户", funcID)
	task2.Params["arg0"] = "Alice"
	if err := storage.SaveTask(ctx, task2); err != nil {
		log.Fatalf("保存任务失败: %v", err)
	}
	fmt.Printf("✓ 保存任务: %s (通过函数ID创建)\n", task2.Name)

	// 继续创建其他任务
	task3, err := NewTaskWithFunction(ctx, registry,
		"数据处理任务",
		"处理数据",
		ProcessData,
		"ProcessData",
		"处理数据",
	)
	if err != nil {
		log.Fatalf("创建任务失败: %v", err)
	}
	task3.Params["arg0"] = "test data"
	task3.Params["arg1"] = "5"
	if err := storage.SaveTask(ctx, task3); err != nil {
		log.Fatalf("保存任务失败: %v", err)
	}
	fmt.Printf("✓ 保存任务: %s\n", task3.Name)

	task4, err := NewTaskWithFunction(ctx, registry,
		"文件下载任务",
		"下载文件",
		DownloadFile,
		"DownloadFile",
		"下载文件",
	)
	if err != nil {
		log.Fatalf("创建任务失败: %v", err)
	}
	task4.Params["arg0"] = "https://example.com/file.zip"
	task4.Params["arg1"] = "3"
	if err := storage.SaveTask(ctx, task4); err != nil {
		log.Fatalf("保存任务失败: %v", err)
	}
	fmt.Printf("✓ 保存任务: %s\n", task4.Name)

	// 测试自定义类型任务（这些可能会失败，因为转换器不支持）
	fmt.Println("\n=== 测试自定义类型任务 ===")

	// 任务5: ProcessOrder (UserID + Order struct)
	task5, err := NewTaskWithFunction(ctx, registry,
		"处理订单任务",
		"处理订单（自定义类型测试）",
		ProcessOrder,
		"ProcessOrder",
		"处理订单",
	)
	if err != nil {
		fmt.Printf("⚠ 创建ProcessOrder任务失败: %v\n", err)
	} else {
		task5.Params["arg0"] = "user123"                                             // UserID类型
		task5.Params["arg1"] = `{"id":"order-001","amount":1000,"status":"pending"}` // Order结构体（JSON）
		if err := storage.SaveTask(ctx, task5); err != nil {
			fmt.Printf("⚠ 保存ProcessOrder任务失败: %v\n", err)
		} else {
			fmt.Printf("✓ 保存任务: %s (自定义类型)\n", task5.Name)
		}
	}

	// 任务6: ProcessUserList (slice)
	task6, err := NewTaskWithFunction(ctx, registry,
		"处理用户列表任务",
		"处理用户列表（slice类型测试）",
		ProcessUserList,
		"ProcessUserList",
		"处理用户列表",
	)
	if err != nil {
		fmt.Printf("⚠ 创建ProcessUserList任务失败: %v\n", err)
	} else {
		task6.Params["arg0"] = `["Alice","Bob","Charlie"]` // slice类型（JSON）
		if err := storage.SaveTask(ctx, task6); err != nil {
			fmt.Printf("⚠ 保存ProcessUserList任务失败: %v\n", err)
		} else {
			fmt.Printf("✓ 保存任务: %s (slice类型)\n", task6.Name)
		}
	}

	// 任务7: ProcessConfig (map)
	task7, err := NewTaskWithFunction(ctx, registry,
		"处理配置任务",
		"处理配置（map类型测试）",
		ProcessConfig,
		"ProcessConfig",
		"处理配置",
	)
	if err != nil {
		fmt.Printf("⚠ 创建ProcessConfig任务失败: %v\n", err)
	} else {
		task7.Params["arg0"] = `{"key1":"value1","key2":"value2","key3":"value3"}` // map类型（JSON）
		if err := storage.SaveTask(ctx, task7); err != nil {
			fmt.Printf("⚠ 保存ProcessConfig任务失败: %v\n", err)
		} else {
			fmt.Printf("✓ 保存任务: %s (map类型)\n", task7.Name)
		}
	}

	// 任务8: ProcessUserProfile (嵌套结构体)
	task8, err := NewTaskWithFunction(ctx, registry,
		"处理用户资料任务",
		"处理用户资料（嵌套结构体测试）",
		ProcessUserProfile,
		"ProcessUserProfile",
		"处理用户资料",
	)
	if err != nil {
		fmt.Printf("⚠ 创建ProcessUserProfile任务失败: %v\n", err)
	} else {
		task8.Params["arg0"] = `{"user_id":"user456","name":"John Doe","email":"john@example.com","settings":{"theme":"dark","lang":"en"},"tags":["vip","active"]}` // 嵌套结构体（JSON）
		if err := storage.SaveTask(ctx, task8); err != nil {
			fmt.Printf("⚠ 保存ProcessUserProfile任务失败: %v\n", err)
		} else {
			fmt.Printf("✓ 保存任务: %s (嵌套结构体)\n", task8.Name)
		}
	}

	fmt.Println("\n=== 保存完成 ===")
	fmt.Printf("数据库文件: %s\n", dbPath)
	fmt.Printf("已注册函数数: %d\n", len(registry.metaMap))
}

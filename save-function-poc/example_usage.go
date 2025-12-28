package main

import (
	"context"
	"fmt"
	"log"
)

// 这是一个示例文件，展示用户如何使用这个库

// ExampleUserMain 展示用户在main.go中的使用方式
func ExampleUserMain() {
	ctx := context.Background()
	dbPath := "tasks.db"

	// 1. 创建存储和注册中心
	storage, err := NewStorage(dbPath)
	if err != nil {
		log.Fatalf("创建存储失败: %v", err)
	}
	defer storage.Close()

	registry := NewFunctionRegistry(storage)

	// 2. 在程序启动时，批量注册所有函数
	// 方式1: 使用RegisterBatch批量注册
	functions := []FunctionDef{
		{Name: "AddNumbers", Description: "两个数字相加", Function: AddNumbers},
		{Name: "GreetUser", Description: "问候用户", Function: GreetUser},
		{Name: "ProcessData", Description: "处理数据", Function: ProcessData},
		{Name: "DownloadFile", Description: "下载文件", Function: DownloadFile},
	}

	if err := registry.RegisterBatch(ctx, functions); err != nil {
		log.Fatalf("批量注册函数失败: %v", err)
	}
	fmt.Println("✓ 批量注册函数完成")

	// 3. 从数据库恢复已存在的函数（如果程序重启）
	funcMap := map[string]interface{}{
		"AddNumbers":   AddNumbers,
		"GreetUser":    GreetUser,
		"ProcessData":  ProcessData,
		"DownloadFile": DownloadFile,
	}

	if err := registry.RestoreFunctions(ctx, funcMap); err != nil {
		log.Fatalf("恢复函数失败: %v", err)
	}
	fmt.Println("✓ 恢复函数完成")

	// 4. 创建任务 - 方式1: 通过函数引用（自动注册）
	task1, err := NewTaskWithFunction(ctx, registry,
		"计算任务",
		"计算两个数字的和",
		AddNumbers,
		"AddNumbers", // 函数名称
		"两个数字相加",     // 函数描述
	)
	if err != nil {
		log.Fatalf("创建任务失败: %v", err)
	}
	task1.Params["arg0"] = "10"
	task1.Params["arg1"] = "20"

	// 5. 创建任务 - 方式2: 通过函数ID（函数已注册）
	funcID := registry.GetIDByName("GreetUser")
	if funcID == "" {
		log.Fatal("函数未找到")
	}
	task2 := NewTask("问候任务", "问候用户", funcID)
	task2.Params["arg0"] = "Bob"

	// 6. 保存任务到数据库
	if err := storage.SaveTask(ctx, task1); err != nil {
		log.Fatalf("保存任务失败: %v", err)
	}
	if err := storage.SaveTask(ctx, task2); err != nil {
		log.Fatalf("保存任务失败: %v", err)
	}
	fmt.Println("✓ 任务保存完成")

	// 7. 执行任务
	fmt.Println("\n=== 执行任务 ===")

	// 执行任务1
	fn1 := registry.Get(task1.FuncID)
	if fn1 != nil {
		stateCh := fn1(ctx, task1.Params)
		state := <-stateCh
		if state.Status == "Success" {
			fmt.Printf("任务1执行成功: %v\n", state.Data)
		} else {
			fmt.Printf("任务1执行失败: %v\n", state.Error)
		}
	}

	// 执行任务2
	fn2 := registry.Get(task2.FuncID)
	if fn2 != nil {
		stateCh := fn2(ctx, task2.Params)
		state := <-stateCh
		if state.Status == "Success" {
			fmt.Printf("任务2执行成功: %v\n", state.Data)
		} else {
			fmt.Printf("任务2执行失败: %v\n", state.Error)
		}
	}
}

// ExampleLoadAndRun 展示如何加载并执行已保存的任务
func ExampleLoadAndRun() {
	ctx := context.Background()
	dbPath := "tasks.db"

	// 1. 创建存储和注册中心
	storage, err := NewStorage(dbPath)
	if err != nil {
		log.Fatalf("创建存储失败: %v", err)
	}
	defer storage.Close()

	registry := NewFunctionRegistry(storage)

	// 2. 恢复函数（从数据库加载元数据并注册函数实例）
	funcMap := map[string]interface{}{
		"AddNumbers":   AddNumbers,
		"GreetUser":    GreetUser,
		"ProcessData":  ProcessData,
		"DownloadFile": DownloadFile,
	}

	if err := registry.RestoreFunctions(ctx, funcMap); err != nil {
		log.Fatalf("恢复函数失败: %v", err)
	}
	fmt.Println("✓ 恢复函数完成")

	// 3. 从数据库加载所有任务
	tasks, err := storage.ListAllTasks(ctx)
	if err != nil {
		log.Fatalf("加载任务失败: %v", err)
	}

	// 4. 执行所有任务
	for _, task := range tasks {
		fn := registry.Get(task.FuncID)
		if fn == nil {
			fmt.Printf("⚠ 任务 %s 的函数未找到\n", task.Name)
			continue
		}

		stateCh := fn(ctx, task.Params)
		state := <-stateCh
		if state.Status == "Success" {
			fmt.Printf("✓ 任务 %s 执行成功: %v\n", task.Name, state.Data)
		} else {
			fmt.Printf("❌ 任务 %s 执行失败: %v\n", task.Name, state.Error)
		}
	}
}

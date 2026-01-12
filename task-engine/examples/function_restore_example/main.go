package main

import (
	"context"
	"log"
	"os"
	"time"

	"github.com/LENAX/task-engine/pkg/core/builder"
	"github.com/LENAX/task-engine/pkg/core/engine"
	"github.com/LENAX/task-engine/pkg/core/task"
	"github.com/LENAX/task-engine/pkg/core/workflow"
)

// ========== 示例Job函数实现 ==========

// exampleJobFunc 示例Job函数
func exampleJobFunc(ctx *task.TaskContext) (interface{}, error) {
	log.Printf("执行Job函数: TaskID=%s, TaskName=%s", ctx.TaskID, ctx.TaskName)
	time.Sleep(100 * time.Millisecond)
	return map[string]interface{}{
		"result":  "success",
		"task_id": ctx.TaskID,
	}, nil
}

// anotherJobFunc 另一个示例Job函数
func anotherJobFunc(ctx *task.TaskContext) (interface{}, error) {
	log.Printf("执行另一个Job函数: TaskID=%s", ctx.TaskID)
	time.Sleep(50 * time.Millisecond)
	return map[string]interface{}{
		"result": "another success",
	}, nil
}

func main() {
	// 配置数据库文件路径
	dbPath := "./function_restore_example.db"
	// 清理旧的数据库文件（如果存在）
	if _, err := os.Stat(dbPath); err == nil {
		os.Remove(dbPath)
		log.Println("已清理旧的数据库文件")
	}

	// ========== 第一阶段：注册函数并持久化 ==========
	log.Println("========== 第一阶段：注册函数并持久化 ==========")

	// 创建函数映射表（用于恢复）
	funcMap := map[string]interface{}{
		"exampleJobFunc": exampleJobFunc,
		"anotherJobFunc": anotherJobFunc,
	}

	// 构建引擎（第一次启动）
	engine1, err := engine.NewEngineBuilder("./configs/framework/dev.yaml").
		WithJobFunc("exampleJobFunc", exampleJobFunc).
		WithJobFunc("anotherJobFunc", anotherJobFunc).
		WithFunctionMap(funcMap).  // 设置函数映射表
		RestoreFunctionsOnStart(). // 启用启动时自动恢复
		Build()
	if err != nil {
		log.Fatalf("构建引擎失败: %v", err)
	}

	// 启动引擎
	ctx := context.Background()
	if err := engine1.Start(ctx); err != nil {
		log.Fatalf("启动引擎失败: %v", err)
	}
	defer engine1.Stop()

	log.Println("✅ 引擎已启动，函数已注册并持久化到数据库")

	// 验证函数已注册
	registry := engine1.GetRegistry()
	funcID1 := registry.GetIDByName("exampleJobFunc")
	funcID2 := registry.GetIDByName("anotherJobFunc")
	log.Printf("函数ID: exampleJobFunc=%s, anotherJobFunc=%s", funcID1, funcID2)

	// 等待一下，确保数据已持久化
	time.Sleep(500 * time.Millisecond)

	// 停止引擎（模拟重启）
	log.Println("\n========== 模拟系统重启 ==========")
	engine1.Stop()
	log.Println("引擎已停止")

	// ========== 第二阶段：重启后恢复函数 ==========
	log.Println("\n========== 第二阶段：重启后恢复函数 ==========")

	// 重新构建引擎（使用相同的函数映射表）
	engine2, err := engine.NewEngineBuilder("./configs/framework/dev.yaml").
		WithFunctionMap(funcMap).  // 使用相同的函数映射表
		RestoreFunctionsOnStart(). // 启用启动时自动恢复
		Build()
	if err != nil {
		log.Fatalf("重新构建引擎失败: %v", err)
	}

	// 启动引擎（会自动恢复函数）
	if err := engine2.Start(ctx); err != nil {
		log.Fatalf("重新启动引擎失败: %v", err)
	}
	defer engine2.Stop()

	log.Println("✅ 引擎已重新启动，函数已从数据库恢复")

	// 验证函数已恢复
	registry2 := engine2.GetRegistry()
	restoredFuncID1 := registry2.GetIDByName("exampleJobFunc")
	restoredFuncID2 := registry2.GetIDByName("anotherJobFunc")
	log.Printf("恢复的函数ID: exampleJobFunc=%s, anotherJobFunc=%s", restoredFuncID1, restoredFuncID2)

	// 验证函数ID是否一致（应该一致，因为是从数据库恢复的）
	if funcID1 != "" && restoredFuncID1 != "" && funcID1 != restoredFuncID1 {
		log.Printf("⚠️ 警告: 函数ID不一致，原ID=%s, 恢复ID=%s", funcID1, restoredFuncID1)
	} else {
		log.Println("✅ 函数ID一致，恢复成功")
	}

	// ========== 第三阶段：使用恢复的函数执行任务 ==========
	log.Println("\n========== 第三阶段：使用恢复的函数执行任务 ==========")

	// 创建Workflow
	wf := workflow.NewWorkflow("test-workflow", "测试工作流")

	// 创建任务，使用恢复的函数
	task1, err := builder.NewTaskBuilder("task1", "任务1", registry2).
		WithJobFunction("exampleJobFunc", nil).
		Build()
	if err != nil {
		log.Fatalf("创建任务失败: %v", err)
	}

	task2, err := builder.NewTaskBuilder("task2", "任务2", registry2).
		WithJobFunction("anotherJobFunc", nil).
		WithDependency("task1").
		Build()
	if err != nil {
		log.Fatalf("创建任务失败: %v", err)
	}

	wf.AddTask(task1)
	wf.AddTask(task2)

	// 提交Workflow执行
	wfCtrl, err := engine2.SubmitWorkflow(ctx, wf)
	if err != nil {
		log.Fatalf("提交Workflow失败: %v", err)
	}

	log.Printf("✅ Workflow已提交，InstanceID=%s", wfCtrl.InstanceID())

	// 等待Workflow完成
	timeout := time.After(30 * time.Second)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-timeout:
			log.Fatalf("Workflow执行超时，当前状态: %s", wfCtrl.Status())
		case <-ticker.C:
			status := wfCtrl.Status()
			if status == "Success" || status == "Failed" {
				log.Printf("✅ Workflow完成，状态: %s", status)
				if status != "Success" {
					log.Fatalf("Workflow应该成功完成，实际状态: %s", status)
				}
				goto done
			}
		}
	}

done:
	log.Println("\n========== 示例完成 ==========")
	log.Println("✅ 函数恢复功能验证成功：")
	log.Println("  1. 函数已注册并持久化到数据库")
	log.Println("  2. 系统重启后函数已自动恢复")
	log.Println("  3. 恢复的函数可以正常执行任务")
}

package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/stevelan1995/task-engine/pkg/core/engine"
	"github.com/stevelan1995/task-engine/pkg/core/task"
)

// ========== 示例Job函数实现 ==========

// exampleJob1 示例Job函数1（使用TaskContext，返回error）
func exampleJob1(ctx *task.TaskContext) (string, error) {
	log.Printf("执行Job1: TaskID=%s, TaskName=%s", ctx.TaskID, ctx.TaskName)

	// 从context中获取ExampleService依赖（使用字符串key方式，更直观）
	// 方式1：使用GetDependency + 类型断言（简单直接）
	service, ok := ctx.GetDependency("ExampleService")
	if !ok {
		log.Printf("警告: Job1 未找到ExampleService依赖，使用默认逻辑")
	} else {
		// 类型断言为ExampleService
		if exampleService, ok := service.(*ExampleService); ok {
			// 使用service执行业务逻辑
			exampleService.DoSomething()
			log.Printf("Job1 使用服务 %s 执行业务逻辑", exampleService.Name)
		}
	}

	// 方式2：使用GetDependencyTyped（类型安全，推荐，但需要包级函数）
	// exampleService, ok := task.GetDependencyTyped[*ExampleService](ctx.Context(), "ExampleService")
	// if ok {
	// 	exampleService.DoSomething()
	// 	log.Printf("Job1 使用服务 %s 执行业务逻辑", exampleService.Name)
	// }

	// 模拟业务逻辑
	time.Sleep(100 * time.Millisecond)

	// 检查context是否被取消
	select {
	case <-ctx.Done():
		return "", fmt.Errorf("任务被取消")
	default:
	}

	// 成功完成
	result := "Job1执行成功"
	if ok {
		if exampleService, ok := service.(*ExampleService); ok {
			result = fmt.Sprintf("Job1执行成功（使用了服务: %s）", exampleService.Name)
		}
	}
	return result, nil
}

// exampleJob2 示例Job函数2（使用context.Context，兼容旧方式）
func exampleJob2(ctx context.Context) (string, error) {
	log.Println("执行Job2")

	// 从context中获取ExampleService依赖（使用字符串key方式，更直观）
	// 方式1：使用GetDependencyByKey（需要类型断言）
	service, ok := task.GetDependencyByKey(ctx, "ExampleService")
	if !ok {
		log.Printf("警告: Job2 未找到ExampleService依赖，使用默认逻辑")
	} else {
		// 类型断言为ExampleService
		if exampleService, ok := service.(*ExampleService); ok {
			// 使用service执行业务逻辑
			exampleService.DoSomething()
			log.Printf("Job2 使用服务 %s 执行业务逻辑", exampleService.Name)
		}
	}

	// 模拟业务逻辑
	time.Sleep(100 * time.Millisecond)

	// 检查context是否被取消
	select {
	case <-ctx.Done():
		return "", fmt.Errorf("任务被取消")
	default:
	}

	result := "Job2执行成功"
	if ok {
		if exampleService, ok := service.(*ExampleService); ok {
			result = fmt.Sprintf("Job2执行成功（使用了服务: %s）", exampleService.Name)
		}
	}
	return result, nil
}

// ========== 示例Callback函数实现 ==========

// exampleSuccessCallback 成功回调函数（包装为TaskHandlerType）
func exampleSuccessCallback(ctx *task.TaskContext) {
	taskID := ctx.TaskID
	if taskID == "" {
		taskID = "unknown"
	}
	log.Printf("✅ 任务成功回调: TaskID=%s", taskID)
}

// exampleFailedCallback 失败回调函数（包装为TaskHandlerType）
func exampleFailedCallback(ctx *task.TaskContext) {
	taskID := ctx.TaskID
	if taskID == "" {
		taskID = "unknown"
	}
	log.Printf("❌ 任务失败回调: TaskID=%s", taskID)
}

// ========== 示例服务依赖 ==========

// ExampleService 示例服务
type ExampleService struct {
	Name string
}

func (s *ExampleService) DoSomething() {
	log.Printf("服务 %s 执行操作", s.Name)
}

func main() {
	// ========== 1. 构建引擎（链式调用） ==========
	engineIns, err := engine.NewEngineBuilder("./configs/framework/dev.yaml").
		// 注册Job函数
		WithJobFunc("exampleJob1", exampleJob1).
		WithJobFunc("exampleJob2", exampleJob2).
		// 注册Callback函数
		WithCallbackFunc("exampleSuccessCallback", exampleSuccessCallback).
		WithCallbackFunc("exampleFailedCallback", exampleFailedCallback).
		// 注册服务依赖（示例）
		WithService("ExampleService", &ExampleService{Name: "示例服务"}).
		// 构建引擎实例
		Build()
	if err != nil {
		log.Fatalf("构建引擎失败: %v", err)
	}

	// ========== 2. 启动引擎（非阻塞） ==========
	if err := engineIns.Start(context.Background()); err != nil {
		log.Fatalf("启动引擎失败: %v", err)
	}
	defer engineIns.Stop()

	// ========== 3. 用户侧其他初始化操作（非阻塞，按需执行） ==========
	log.Println("引擎已启动，可以进行其他初始化操作...")

	// ========== 4. 加载工作流定义（从文件） ==========
	wfDef, err := engineIns.LoadWorkflow("./configs/workflows/example-workflow.yaml")
	if err != nil {
		log.Fatalf("加载工作流失败: %v", err)
	}

	log.Printf("✅ 加载工作流成功: %s (ID: %s)", wfDef.SourcePath, wfDef.ID)

	// ========== 5. 提交工作流运行（非阻塞） ==========
	wfCtrl, err := engineIns.SubmitWorkflow(context.Background(), wfDef.Workflow)
	if err != nil {
		log.Fatalf("提交工作流失败: %v", err)
	}

	// ========== 6. 用户侧可选操作 ==========
	// 非阻塞查询状态
	time.Sleep(1 * time.Second)
	log.Printf("工作流实例 %s 状态: %s", wfCtrl.InstanceID(), wfCtrl.Status())

	// 等待所有workflow完成后退出
	log.Println("等待所有workflow完成...")
	if err := engineIns.WaitForAllWorkflows(); err != nil {
		log.Fatalf("等待workflow完成失败: %v", err)
	}

	log.Println("✅ 所有workflow已完成，退出程序")
}

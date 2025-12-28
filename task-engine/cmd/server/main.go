package main

import (
	"github.com/stevelan1995/task-engine/pkg/core/engine"
	// "github.com/stevelan1995/task-engine/pkg/core/workflow"
	"context"
	"log"

	"github.com/stevelan1995/task-engine/internal/storage/sqlite"
	"github.com/stevelan1995/task-engine/pkg/core/builder"
)

func main() {
	// 1. 创建存储接口实例（内部实现，对外仅依赖接口）
	repo, err := sqlite.NewWorkflowRepo("./data.db")
	if err != nil {
		log.Fatal("创建存储失败:", err)
	}

	// 2. 创建引擎（调用对外核心组件）
	eng, err := engine.NewEngine(100, 30, repo)
	if err != nil {
		log.Fatal("创建引擎失败:", err)
	}

	// 3. 启动引擎
	if err := eng.Start(context.Background()); err != nil {
		log.Fatal("启动引擎失败:", err)
	}
	defer eng.Stop()

	// 4. 构建Workflow（调用对外Builder）
	wf, err := builder.NewWorkflowBuilder("测试任务", "极简结构测试").
		WithParams(map[string]string{"key": "value"}).
		Build()
	if err != nil {
		log.Fatal("构建Workflow失败:", err)
	}

	// 5. 注册Workflow
	if err := eng.RegisterWorkflow(context.Background(), wf); err != nil {
		log.Fatal("注册Workflow失败:", err)
	}

	log.Println("🎉 服务端启动完成（极简结构）")
	select {} // 阻塞运行
}

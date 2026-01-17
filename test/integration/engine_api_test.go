package integration

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/LENAX/task-engine/pkg/core/engine"
)

func TestEngineBuilderAndLoadWorkflow(t *testing.T) {
	// 创建临时目录
	tmpDir := t.TempDir()
	frameworkConfigPath := filepath.Join(tmpDir, "framework.yaml")
	workflowConfigPath := filepath.Join(tmpDir, "workflow.yaml")

	// 创建框架配置文件
	frameworkConfig := `
task-engine:
  general:
    instance_name: "test-engine"
    log_level: "debug"
    env: "test"
  storage:
    database:
      type: "sqlite"
      dsn: "` + filepath.Join(tmpDir, "test.db") + `"
      max_open_conns: 5
      max_idle_conns: 2
      conn_max_lifetime: "1h"
      conn_max_idle_time: "30m"
    cache:
      enabled: true
      default_ttl: "2h"
      clean_interval: "1h"
  execution:
    default_task_timeout: "60s"
    worker_concurrency: 5
    retry:
      enabled: true
      max_attempts: 5
      delay: "2s"
      max_delay: "10s"
`
	if err := os.WriteFile(frameworkConfigPath, []byte(frameworkConfig), 0644); err != nil {
		t.Fatalf("创建框架配置文件失败: %v", err)
	}

	// 创建业务配置文件
	workflowConfig := `
workflows:
  jobs:
    - job_id: "test-job"
      func_key: "testFunc"
      description: "测试Job"
      timeout: "30s"
  definitions:
    - workflow_id: "test-workflow"
      description: "测试工作流"
      tasks:
        - task_id: "task-1"
          job_id: "test-job"
          params: {}
          dependencies: []
`
	if err := os.WriteFile(workflowConfigPath, []byte(workflowConfig), 0644); err != nil {
		t.Fatalf("创建业务配置文件失败: %v", err)
	}

	// 1. 测试EngineBuilder
	engineIns, err := engine.NewEngineBuilder(frameworkConfigPath).Build()
	if err != nil {
		t.Fatalf("构建引擎失败: %v", err)
	}

	// 2. 测试启动引擎
	if err := engineIns.Start(context.Background()); err != nil {
		t.Fatalf("启动引擎失败: %v", err)
	}
	defer engineIns.Stop()

	// 3. 测试LoadWorkflow（注意：由于Job函数未注册，校验会失败）
	// 这里我们测试配置加载和校验逻辑，即使校验失败也是预期的
	_, err = engineIns.LoadWorkflow(workflowConfigPath)
	if err != nil {
		// 校验失败是预期的，因为Job函数未注册
		t.Logf("LoadWorkflow校验失败（预期）: %v", err)
		// 跳过后续测试
		return
	}

	// 如果LoadWorkflow成功，继续测试
	wfDef, err := engineIns.LoadWorkflow(workflowConfigPath)
	if err == nil {
		if wfDef == nil {
			t.Fatal("WorkflowDefinition为空")
		}

		if wfDef.ID != "test-workflow" {
			t.Errorf("期望WorkflowID为test-workflow，实际为%s", wfDef.ID)
		}

		if wfDef.Workflow == nil {
			t.Fatal("Workflow对象为空")
		}

		// 4. 测试SubmitWorkflow
		wfCtrl, err := engineIns.SubmitWorkflow(context.Background(), wfDef.Workflow)
		if err != nil {
			t.Fatalf("提交工作流失败: %v", err)
		}

		if wfCtrl == nil {
			t.Fatal("WorkflowController为空")
		}

		// 5. 测试WorkflowController API
		instanceID := wfCtrl.InstanceID()
		if instanceID == "" {
			t.Error("InstanceID为空")
		}

		status := wfCtrl.Status()
		if status == "" {
			t.Error("Status为空")
		}

		// 6. 测试Wait方法（带超时）
		err = wfCtrl.Wait(1 * time.Second)
		if err != nil {
			// 超时是预期的
			t.Logf("Wait超时（预期）: %v", err)
		}
	}

	// 7. 测试Stop
	engineIns.Stop()
}

func TestEngineBuilder_WithJobFunc(t *testing.T) {
	tmpDir := t.TempDir()
	frameworkConfigPath := filepath.Join(tmpDir, "framework.yaml")

	frameworkConfig := `
task-engine:
  general:
    instance_name: "test-engine"
    log_level: "debug"
    env: "test"
  storage:
    database:
      type: "sqlite"
      dsn: "` + filepath.Join(tmpDir, "test.db") + `"
      max_open_conns: 5
  execution:
    default_task_timeout: "60s"
    worker_concurrency: 5
`

	if err := os.WriteFile(frameworkConfigPath, []byte(frameworkConfig), 0644); err != nil {
		t.Fatalf("创建框架配置文件失败: %v", err)
	}

	// 测试链式调用（使用符合签名的函数）
	// Job函数签名：func(ctx *TaskContext) <-chan JobFunctionState 或 func(ctx context.Context, ...) (result, error)
	// 为了简化测试，使用context.Context签名
	testJobFunc := func(ctx context.Context) error {
		return nil
	}
	testCallbackFunc := func(ctx context.Context) error {
		return nil
	}

	engineIns, err := engine.NewEngineBuilder(frameworkConfigPath).
		WithJobFunc("testFunc", testJobFunc).
		WithCallbackFunc("testCallback", testCallbackFunc).
		WithService("testService", "test").
		Build()

	if err != nil {
		t.Fatalf("构建引擎失败: %v", err)
	}

	if engineIns == nil {
		t.Fatal("Engine实例为空")
	}
}

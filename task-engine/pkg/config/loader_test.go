package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadFrameworkConfig(t *testing.T) {
	// 创建临时配置文件
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "test.yaml")
	configContent := `
task-engine:
  general:
    instance_name: "test-engine"
    log_level: "debug"
    env: "test"
  storage:
    database:
      type: "sqlite"
      dsn: "./test.db"
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
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("创建测试配置文件失败: %v", err)
	}

	// 测试加载配置
	cfg, err := LoadFrameworkConfig(configPath)
	if err != nil {
		t.Fatalf("加载配置失败: %v", err)
	}

	// 验证配置值
	if cfg.TaskEngine.General.InstanceName != "test-engine" {
		t.Errorf("期望instance_name为test-engine，实际为%s", cfg.TaskEngine.General.InstanceName)
	}
	if cfg.TaskEngine.General.LogLevel != "debug" {
		t.Errorf("期望log_level为debug，实际为%s", cfg.TaskEngine.General.LogLevel)
	}
	if cfg.TaskEngine.Storage.Database.Type != "sqlite" {
		t.Errorf("期望database.type为sqlite，实际为%s", cfg.TaskEngine.Storage.Database.Type)
	}
	if cfg.TaskEngine.Execution.WorkerConcurrency != 5 {
		t.Errorf("期望worker_concurrency为5，实际为%d", cfg.TaskEngine.Execution.WorkerConcurrency)
	}
}

func TestLoadFrameworkConfig_WithDefaults(t *testing.T) {
	// 创建最小配置文件（测试默认值填充）
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "minimal.yaml")
	configContent := `
task-engine:
  storage:
    database:
      type: "sqlite"
      dsn: "./test.db"
`
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("创建测试配置文件失败: %v", err)
	}

	cfg, err := LoadFrameworkConfig(configPath)
	if err != nil {
		t.Fatalf("加载配置失败: %v", err)
	}

	// 验证默认值
	if cfg.TaskEngine.General.InstanceName == "" {
		t.Error("期望instance_name有默认值")
	}
	if cfg.TaskEngine.Execution.WorkerConcurrency <= 0 {
		t.Error("期望worker_concurrency有默认值")
	}
	if cfg.TaskEngine.Execution.DefaultTaskTimeout <= 0 {
		t.Error("期望default_task_timeout有默认值")
	}
}

func TestLoadFrameworkConfig_WithEnvVars(t *testing.T) {
	// 设置环境变量
	os.Setenv("TEST_ENV", "test-value")
	os.Setenv("TEST_DB_PATH", "/tmp/test.db")
	defer os.Unsetenv("TEST_ENV")
	defer os.Unsetenv("TEST_DB_PATH")

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "env-test.yaml")
	configContent := `
task-engine:
  general:
    instance_name: "${TEST_ENV}"
    env: "${TEST_ENV}"
  storage:
    database:
      type: "sqlite"
      dsn: "${TEST_DB_PATH}"
`
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("创建测试配置文件失败: %v", err)
	}

	cfg, err := LoadFrameworkConfig(configPath)
	if err != nil {
		t.Fatalf("加载配置失败: %v", err)
	}

	// 验证环境变量替换
	if cfg.TaskEngine.General.InstanceName != "test-value" {
		t.Errorf("期望instance_name为test-value，实际为%s", cfg.TaskEngine.General.InstanceName)
	}
	if cfg.TaskEngine.Storage.Database.DSN != "/tmp/test.db" {
		t.Errorf("期望dsn为/tmp/test.db，实际为%s", cfg.TaskEngine.Storage.Database.DSN)
	}
}

func TestLoadWorkflowConfig(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "workflow.yaml")
	configContent := `
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
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("创建测试配置文件失败: %v", err)
	}

	cfg, err := LoadWorkflowConfig(configPath)
	if err != nil {
		t.Fatalf("加载配置失败: %v", err)
	}

	if len(cfg.Workflows.Jobs) != 1 {
		t.Errorf("期望1个Job，实际为%d", len(cfg.Workflows.Jobs))
	}
	if len(cfg.Workflows.Definitions) != 1 {
		t.Errorf("期望1个Workflow定义，实际为%d", len(cfg.Workflows.Definitions))
	}
}

func TestLoadWorkflowConfigs(t *testing.T) {
	tmpDir := t.TempDir()

	// 创建多个配置文件
	config1 := `
workflows:
  jobs:
    - job_id: "job-1"
      func_key: "func1"
      timeout: "30s"
`
	config2 := `
workflows:
  jobs:
    - job_id: "job-2"
      func_key: "func2"
      timeout: "60s"
  definitions:
    - workflow_id: "wf-1"
      tasks: []
`

	if err := os.WriteFile(filepath.Join(tmpDir, "config1.yaml"), []byte(config1), 0644); err != nil {
		t.Fatalf("创建配置文件失败: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "config2.yaml"), []byte(config2), 0644); err != nil {
		t.Fatalf("创建配置文件失败: %v", err)
	}

	configs, err := LoadWorkflowConfigs(tmpDir)
	if err != nil {
		t.Fatalf("加载配置失败: %v", err)
	}

	if len(configs) != 2 {
		t.Errorf("期望加载2个配置文件，实际为%d", len(configs))
	}
}

func TestMergeWorkflowConfigs(t *testing.T) {
	cfg1 := &WorkflowConfig{}
	cfg1.Workflows.Jobs = []JobDefinition{
		{JobID: "job-1", FuncKey: "func1"},
		{JobID: "job-2", FuncKey: "func2"},
	}
	cfg1.Workflows.Definitions = []WorkflowDefinition{
		{WorkflowID: "wf-1"},
	}

	cfg2 := &WorkflowConfig{}
	cfg2.Workflows.Jobs = []JobDefinition{
		{JobID: "job-2", FuncKey: "func2-updated"}, // 重复的job_id，应该被忽略
		{JobID: "job-3", FuncKey: "func3"},
	}
	cfg2.Workflows.Definitions = []WorkflowDefinition{
		{WorkflowID: "wf-2"},
	}

	merged, err := MergeWorkflowConfigs([]*WorkflowConfig{cfg1, cfg2})
	if err != nil {
		t.Fatalf("合并配置失败: %v", err)
	}

	// 验证Jobs去重（应该只有3个：job-1, job-2, job-3）
	if len(merged.Workflows.Jobs) != 3 {
		t.Errorf("期望合并后有3个Job，实际为%d", len(merged.Workflows.Jobs))
	}

	// 验证WorkflowDefinitions合并
	if len(merged.Workflows.Definitions) != 2 {
		t.Errorf("期望合并后有2个Workflow定义，实际为%d", len(merged.Workflows.Definitions))
	}
}

func TestParseDuration(t *testing.T) {
	tests := []struct {
		input    string
		expected time.Duration
		hasError bool
	}{
		{"30s", 30 * time.Second, false},
		{"1h", 1 * time.Hour, false},
		{"2h30m", 2*time.Hour + 30*time.Minute, false},
		{"", 0, false},
		{"invalid", 0, true},
	}

	for _, tt := range tests {
		result, err := ParseDuration(tt.input)
		if tt.hasError {
			if err == nil {
				t.Errorf("期望ParseDuration(%s)返回错误，但没有错误", tt.input)
			}
		} else {
			if err != nil {
				t.Errorf("ParseDuration(%s)返回错误: %v", tt.input, err)
			}
			if result != tt.expected {
				t.Errorf("ParseDuration(%s)期望%v，实际%v", tt.input, tt.expected, result)
			}
		}
	}
}

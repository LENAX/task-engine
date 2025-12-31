package config

import (
	"testing"
	"time"
)

func TestValidateFrameworkConfig(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *EngineConfig
		wantErr bool
	}{
		{
			name: "有效配置",
			cfg: func() *EngineConfig {
				cfg := &EngineConfig{}
				cfg.TaskEngine.General.InstanceName = "test"
				cfg.TaskEngine.General.LogLevel = "info"
				cfg.TaskEngine.General.Env = "dev"
				cfg.TaskEngine.Storage.Database.Type = "sqlite"
				cfg.TaskEngine.Storage.Database.DSN = "./test.db"
				cfg.TaskEngine.Storage.Database.MaxOpenConns = 10
				cfg.TaskEngine.Execution.DefaultTaskTimeout = 30 * time.Second
				cfg.TaskEngine.Execution.WorkerConcurrency = 10
				return cfg
			}(),
			wantErr: false,
		},
		{
			name:    "空配置",
			cfg:     nil,
			wantErr: true,
		},
		{
			name: "无效的数据库类型",
			cfg: func() *EngineConfig {
				cfg := &EngineConfig{}
				cfg.TaskEngine.General.InstanceName = "test"
				cfg.TaskEngine.Storage.Database.Type = "invalid"
				cfg.TaskEngine.Storage.Database.DSN = "./test.db"
				cfg.TaskEngine.Storage.Database.MaxOpenConns = 10
				cfg.TaskEngine.Execution.DefaultTaskTimeout = 30 * time.Second
				cfg.TaskEngine.Execution.WorkerConcurrency = 10
				return cfg
			}(),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateFrameworkConfig(tt.cfg)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateFrameworkConfig() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateWorkflowConfig(t *testing.T) {
	jobRegistry := map[string]interface{}{
		"func1": func() {},
		"func2": func() {},
	}

	tests := []struct {
		name    string
		cfg     *WorkflowConfig
		wantErr bool
	}{
		{
			name: "有效配置",
			cfg: func() *WorkflowConfig {
				cfg := &WorkflowConfig{}
				cfg.Workflows.Jobs = []JobDefinition{
					{JobID: "job-1", FuncKey: "func1", Timeout: 30 * time.Second},
				}
				cfg.Workflows.Definitions = []WorkflowDefinition{
					{
						WorkflowID: "wf-1",
						Tasks: []TaskDefinition{
							{TaskID: "task-1", JobID: "job-1", Dependencies: []string{}},
						},
					},
				}
				return cfg
			}(),
			wantErr: false,
		},
		{
			name: "重复的job_id",
			cfg: func() *WorkflowConfig {
				cfg := &WorkflowConfig{}
				cfg.Workflows.Jobs = []JobDefinition{
					{JobID: "job-1", FuncKey: "func1"},
					{JobID: "job-1", FuncKey: "func2"}, // 重复
				}
				return cfg
			}(),
			wantErr: true,
		},
		{
			name: "未注册的func_key",
			cfg: func() *WorkflowConfig {
				cfg := &WorkflowConfig{}
				cfg.Workflows.Jobs = []JobDefinition{
					{JobID: "job-1", FuncKey: "unregistered-func"},
				}
				return cfg
			}(),
			wantErr: true,
		},
		{
			name: "Task依赖不存在的Job",
			cfg: func() *WorkflowConfig {
				cfg := &WorkflowConfig{}
				cfg.Workflows.Jobs = []JobDefinition{
					{JobID: "job-1", FuncKey: "func1"},
				}
				cfg.Workflows.Definitions = []WorkflowDefinition{
					{
						WorkflowID: "wf-1",
						Tasks: []TaskDefinition{
							{TaskID: "task-1", JobID: "non-existent-job"},
						},
					},
				}
				return cfg
			}(),
			wantErr: true,
		},
		{
			name: "Task依赖自己",
			cfg: func() *WorkflowConfig {
				cfg := &WorkflowConfig{}
				cfg.Workflows.Jobs = []JobDefinition{
					{JobID: "job-1", FuncKey: "func1"},
				}
				cfg.Workflows.Definitions = []WorkflowDefinition{
					{
						WorkflowID: "wf-1",
						Tasks: []TaskDefinition{
							{TaskID: "task-1", JobID: "job-1", Dependencies: []string{"task-1"}},
						},
					},
				}
				return cfg
			}(),
			wantErr: true,
		},
		{
			name: "无效的callback state",
			cfg: func() *WorkflowConfig {
				cfg := &WorkflowConfig{}
				cfg.Workflows.Jobs = []JobDefinition{
					{JobID: "job-1", FuncKey: "func1"},
				}
				cfg.Workflows.Definitions = []WorkflowDefinition{
					{
						WorkflowID: "wf-1",
						Tasks: []TaskDefinition{
							{
								TaskID: "task-1",
								JobID:  "job-1",
								Callbacks: []CallbackDefinition{
									{State: "invalid-state", FuncKey: "func1"},
								},
							},
						},
					},
				}
				return cfg
			}(),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateWorkflowConfig(tt.cfg, jobRegistry, 30*time.Second)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateWorkflowConfig() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

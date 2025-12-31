package config

import (
	"time"
)

// WorkflowConfig 业务配置（对外导出）
type WorkflowConfig struct {
	Workflows struct {
		Jobs        []JobDefinition      `yaml:"jobs"`
		Definitions []WorkflowDefinition `yaml:"definitions"`
	} `yaml:"workflows"`
}

// JobDefinition Job定义
type JobDefinition struct {
	JobID       string        `yaml:"job_id"`
	FuncKey     string        `yaml:"func_key"`
	Description string        `yaml:"description"`
	Timeout     time.Duration `yaml:"timeout"`
}

// WorkflowDefinition Workflow定义
type WorkflowDefinition struct {
	WorkflowID  string           `yaml:"workflow_id"`
	Description string           `yaml:"description"`
	Tasks       []TaskDefinition `yaml:"tasks"`
}

// TaskDefinition Task定义
type TaskDefinition struct {
	TaskID       string               `yaml:"task_id"`
	JobID        string               `yaml:"job_id"`
	Params       map[string]string    `yaml:"params"`
	Dependencies []string             `yaml:"dependencies"`
	Callbacks    []CallbackDefinition `yaml:"callbacks"`
}

// CallbackDefinition Callback定义
type CallbackDefinition struct {
	State       string `yaml:"state"` // success/failed/timeout
	FuncKey     string `yaml:"func_key"`
	Description string `yaml:"description"`
}

// GetJobByID 根据JobID获取Job定义
func (c *WorkflowConfig) GetJobByID(jobID string) *JobDefinition {
	for i := range c.Workflows.Jobs {
		if c.Workflows.Jobs[i].JobID == jobID {
			return &c.Workflows.Jobs[i]
		}
	}
	return nil
}

// GetWorkflowByID 根据WorkflowID获取Workflow定义
func (c *WorkflowConfig) GetWorkflowByID(workflowID string) *WorkflowDefinition {
	for i := range c.Workflows.Definitions {
		if c.Workflows.Definitions[i].WorkflowID == workflowID {
			return &c.Workflows.Definitions[i]
		}
	}
	return nil
}

// ApplyDefaults 应用默认值（基于框架配置）
func (c *WorkflowConfig) ApplyDefaults(defaultTimeout time.Duration) {
	for i := range c.Workflows.Jobs {
		if c.Workflows.Jobs[i].Timeout <= 0 {
			c.Workflows.Jobs[i].Timeout = defaultTimeout
		}
	}
}

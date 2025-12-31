package config

import (
	"fmt"
	"time"
)

// ValidateFrameworkConfig 校验框架配置合法性
func ValidateFrameworkConfig(cfg *EngineConfig) error {
	if cfg == nil {
		return fmt.Errorf("配置不能为空")
	}

	// 校验General
	if cfg.TaskEngine.General.InstanceName == "" {
		return fmt.Errorf("instance_name不能为空")
	}
	if cfg.TaskEngine.General.LogLevel != "" {
		validLevels := map[string]bool{
			"debug": true,
			"info":  true,
			"warn":  true,
			"error": true,
		}
		if !validLevels[cfg.TaskEngine.General.LogLevel] {
			return fmt.Errorf("log_level必须是debug/info/warn/error之一")
		}
	}

	// 校验Storage.Database
	if cfg.TaskEngine.Storage.Database.Type == "" {
		return fmt.Errorf("database.type不能为空")
	}
	validDBTypes := map[string]bool{
		"sqlite":     true,
		"postgres":   true,
		"postgresql": true,
		"mysql":      true,
	}
	if !validDBTypes[cfg.TaskEngine.Storage.Database.Type] {
		return fmt.Errorf("database.type必须是sqlite/postgres/mysql之一")
	}

	if cfg.TaskEngine.Storage.Database.DSN == "" {
		return fmt.Errorf("database.dsn不能为空")
	}

	if cfg.TaskEngine.Storage.Database.MaxOpenConns <= 0 {
		return fmt.Errorf("database.max_open_conns必须大于0")
	}

	if cfg.TaskEngine.Storage.Database.MaxIdleConns < 0 {
		return fmt.Errorf("database.max_idle_conns不能为负数")
	}

	// 校验Execution
	if cfg.TaskEngine.Execution.WorkerConcurrency <= 0 {
		return fmt.Errorf("execution.worker_concurrency必须大于0")
	}

	if cfg.TaskEngine.Execution.DefaultTaskTimeout <= 0 {
		return fmt.Errorf("execution.default_task_timeout必须大于0")
	}

	// 校验Retry
	if cfg.TaskEngine.Execution.Retry.Enabled {
		if cfg.TaskEngine.Execution.Retry.MaxAttempts < 0 {
			return fmt.Errorf("execution.retry.max_attempts不能为负数")
		}
		if cfg.TaskEngine.Execution.Retry.Delay < 0 {
			return fmt.Errorf("execution.retry.delay不能为负数")
		}
		if cfg.TaskEngine.Execution.Retry.MaxDelay < 0 {
			return fmt.Errorf("execution.retry.max_delay不能为负数")
		}
		if cfg.TaskEngine.Execution.Retry.MaxDelay > 0 &&
			cfg.TaskEngine.Execution.Retry.Delay > cfg.TaskEngine.Execution.Retry.MaxDelay {
			return fmt.Errorf("execution.retry.delay不能大于max_delay")
		}
	}

	return nil
}

// ValidateWorkflowConfig 校验业务配置合法性
func ValidateWorkflowConfig(cfg *WorkflowConfig, jobRegistry map[string]interface{}, defaultTimeout time.Duration) error {
	if cfg == nil {
		return fmt.Errorf("配置不能为空")
	}

	// 校验Jobs
	jobIDMap := make(map[string]bool)
	for i, job := range cfg.Workflows.Jobs {
		if job.JobID == "" {
			return fmt.Errorf("jobs[%d].job_id不能为空", i)
		}
		if jobIDMap[job.JobID] {
			return fmt.Errorf("jobs中存在重复的job_id: %s", job.JobID)
		}
		jobIDMap[job.JobID] = true

		if job.FuncKey == "" {
			return fmt.Errorf("jobs[%d].func_key不能为空", i)
		}

		// 如果提供了jobRegistry，校验func_key是否已注册
		if jobRegistry != nil {
			if _, exists := jobRegistry[job.FuncKey]; !exists {
				return fmt.Errorf("jobs[%d].func_key %s 未在jobRegistry中注册", i, job.FuncKey)
			}
		}

		// 校验超时时间（不能超过默认超时的3倍）
		if job.Timeout > 0 && defaultTimeout > 0 {
			maxTimeout := defaultTimeout * 3
			if job.Timeout > maxTimeout {
				return fmt.Errorf("jobs[%d].timeout %v 超过最大允许值 %v", i, job.Timeout, maxTimeout)
			}
		}
	}

	// 校验WorkflowDefinitions
	workflowIDMap := make(map[string]bool)
	for i, wf := range cfg.Workflows.Definitions {
		if wf.WorkflowID == "" {
			return fmt.Errorf("definitions[%d].workflow_id不能为空", i)
		}
		if workflowIDMap[wf.WorkflowID] {
			return fmt.Errorf("definitions中存在重复的workflow_id: %s", wf.WorkflowID)
		}
		workflowIDMap[wf.WorkflowID] = true

		// 校验Tasks
		taskIDMap := make(map[string]bool)
		for j, task := range wf.Tasks {
			if task.TaskID == "" {
				return fmt.Errorf("definitions[%d].tasks[%d].task_id不能为空", i, j)
			}
			if taskIDMap[task.TaskID] {
				return fmt.Errorf("definitions[%d].tasks中存在重复的task_id: %s", i, task.TaskID)
			}
			taskIDMap[task.TaskID] = true

			if task.JobID == "" {
				return fmt.Errorf("definitions[%d].tasks[%d].job_id不能为空", i, j)
			}

			// 校验JobID是否存在
			if !jobIDMap[task.JobID] {
				return fmt.Errorf("definitions[%d].tasks[%d].job_id %s 不存在于jobs中", i, j, task.JobID)
			}

			// 校验依赖关系（不能依赖自己）
			for k, dep := range task.Dependencies {
				if dep == task.TaskID {
					return fmt.Errorf("definitions[%d].tasks[%d].dependencies[%d] 不能依赖自己", i, j, k)
				}
				// 检查依赖的Task是否存在
				if !taskIDMap[dep] {
					// 注意：这里只检查当前Workflow内的依赖，不检查跨Workflow依赖
					// 如果需要检查跨Workflow依赖，需要传入所有Workflow的Task列表
				}
			}

			// 校验Callbacks
			for k, callback := range task.Callbacks {
				validStates := map[string]bool{
					"success": true,
					"failed":  true,
					"timeout": true,
				}
				if !validStates[callback.State] {
					return fmt.Errorf("definitions[%d].tasks[%d].callbacks[%d].state必须是success/failed/timeout之一", i, j, k)
				}

				if callback.FuncKey == "" {
					return fmt.Errorf("definitions[%d].tasks[%d].callbacks[%d].func_key不能为空", i, j, k)
				}
			}
		}
	}

	return nil
}

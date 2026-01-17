package config

import (
	"time"
)

// EngineConfig 引擎框架配置（对外导出）
type EngineConfig struct {
	TaskEngine struct {
		General struct {
			InstanceName string `yaml:"instance_name"`
			LogLevel     string `yaml:"log_level"`
			Env          string `yaml:"env"`
		} `yaml:"general"`
		Storage struct {
			Database struct {
				Type            string        `yaml:"type"`
				DSN             string        `yaml:"dsn"`
				MaxOpenConns    int           `yaml:"max_open_conns"`
				MaxIdleConns    int           `yaml:"max_idle_conns"`
				ConnMaxLifetime time.Duration `yaml:"conn_max_lifetime"`
				ConnMaxIdleTime time.Duration `yaml:"conn_max_idle_time"`
			} `yaml:"database"`
			Cache struct {
				Enabled       bool          `yaml:"enabled"`
				DefaultTTL    time.Duration `yaml:"default_ttl"`
				CleanInterval time.Duration `yaml:"clean_interval"`
			} `yaml:"cache"`
		} `yaml:"storage"`
		Execution struct {
			DefaultTaskTimeout time.Duration `yaml:"default_task_timeout"`
			WorkerConcurrency  int           `yaml:"worker_concurrency"`
			Retry              struct {
				Enabled     bool          `yaml:"enabled"`
				MaxAttempts int           `yaml:"max_attempts"`
				Delay       time.Duration `yaml:"delay"`
				MaxDelay    time.Duration `yaml:"max_delay"`
			} `yaml:"retry"`
		} `yaml:"execution"`
	} `yaml:"task-engine"`
}

// GetDatabaseType 获取数据库类型
func (c *EngineConfig) GetDatabaseType() string {
	return c.TaskEngine.Storage.Database.Type
}

// GetDatabaseDSN 获取数据库DSN
func (c *EngineConfig) GetDatabaseDSN() string {
	return c.TaskEngine.Storage.Database.DSN
}

// GetWorkerConcurrency 获取Worker并发数
func (c *EngineConfig) GetWorkerConcurrency() int {
	concurrency := c.TaskEngine.Execution.WorkerConcurrency
	if concurrency <= 0 {
		return 10 // 默认值
	}
	return concurrency
}

// GetDefaultTaskTimeout 获取默认任务超时时间
func (c *EngineConfig) GetDefaultTaskTimeout() time.Duration {
	timeout := c.TaskEngine.Execution.DefaultTaskTimeout
	if timeout <= 0 {
		return 30 * time.Second // 默认值
	}
	return timeout
}

// ApplyDefaults 应用默认值
func (c *EngineConfig) ApplyDefaults() {
	// General默认值
	if c.TaskEngine.General.InstanceName == "" {
		c.TaskEngine.General.InstanceName = "task-engine"
	}
	if c.TaskEngine.General.LogLevel == "" {
		c.TaskEngine.General.LogLevel = "info"
	}
	if c.TaskEngine.General.Env == "" {
		c.TaskEngine.General.Env = "dev"
	}

	// Database默认值
	if c.TaskEngine.Storage.Database.MaxOpenConns <= 0 {
		c.TaskEngine.Storage.Database.MaxOpenConns = 10
	}
	if c.TaskEngine.Storage.Database.MaxIdleConns <= 0 {
		c.TaskEngine.Storage.Database.MaxIdleConns = 5
	}
	if c.TaskEngine.Storage.Database.ConnMaxLifetime <= 0 {
		c.TaskEngine.Storage.Database.ConnMaxLifetime = 2 * time.Hour
	}
	if c.TaskEngine.Storage.Database.ConnMaxIdleTime <= 0 {
		c.TaskEngine.Storage.Database.ConnMaxIdleTime = 1 * time.Hour
	}

	// Cache默认值
	if c.TaskEngine.Storage.Cache.DefaultTTL <= 0 {
		c.TaskEngine.Storage.Cache.DefaultTTL = 1 * time.Hour
	}
	if c.TaskEngine.Storage.Cache.CleanInterval <= 0 {
		c.TaskEngine.Storage.Cache.CleanInterval = 30 * time.Minute
	}

	// Execution默认值
	if c.TaskEngine.Execution.DefaultTaskTimeout <= 0 {
		c.TaskEngine.Execution.DefaultTaskTimeout = 30 * time.Second
	}
	if c.TaskEngine.Execution.WorkerConcurrency <= 0 {
		c.TaskEngine.Execution.WorkerConcurrency = 10
	}

	// Retry默认值
	if c.TaskEngine.Execution.Retry.MaxAttempts <= 0 {
		c.TaskEngine.Execution.Retry.MaxAttempts = 3
	}
	if c.TaskEngine.Execution.Retry.Delay <= 0 {
		c.TaskEngine.Execution.Retry.Delay = 1 * time.Second
	}
	if c.TaskEngine.Execution.Retry.MaxDelay <= 0 {
		c.TaskEngine.Execution.Retry.MaxDelay = 5 * time.Second
	}
}

package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// LoadFrameworkConfig 加载框架配置
func LoadFrameworkConfig(path string) (*EngineConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取配置文件失败: %w", err)
	}

	// 环境变量替换
	data = replaceEnvVars(data)

	var cfg EngineConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("解析配置文件失败: %w", err)
	}

	// 应用默认值
	cfg.ApplyDefaults()

	return &cfg, nil
}

// LoadWorkflowConfig 加载业务配置（单文件）
func LoadWorkflowConfig(path string) (*WorkflowConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取配置文件失败: %w", err)
	}

	// 环境变量替换
	data = replaceEnvVars(data)

	var cfg WorkflowConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("解析配置文件失败: %w", err)
	}

	return &cfg, nil
}

// LoadWorkflowConfigs 批量加载业务配置（支持多文件合并）
func LoadWorkflowConfigs(dir string) ([]*WorkflowConfig, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("读取配置目录失败: %w", err)
	}

	var configs []*WorkflowConfig
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		// 只处理.yaml和.yml文件
		if !strings.HasSuffix(strings.ToLower(entry.Name()), ".yaml") &&
			!strings.HasSuffix(strings.ToLower(entry.Name()), ".yml") {
			continue
		}

		path := filepath.Join(dir, entry.Name())
		cfg, err := LoadWorkflowConfig(path)
		if err != nil {
			return nil, fmt.Errorf("加载配置文件 %s 失败: %w", path, err)
		}

		configs = append(configs, cfg)
	}

	return configs, nil
}

// MergeWorkflowConfigs 合并多个WorkflowConfig
func MergeWorkflowConfigs(configs []*WorkflowConfig) (*WorkflowConfig, error) {
	if len(configs) == 0 {
		return &WorkflowConfig{}, nil
	}

	merged := &WorkflowConfig{}
	merged.Workflows.Jobs = make([]JobDefinition, 0)
	merged.Workflows.Definitions = make([]WorkflowDefinition, 0)

	// 合并Jobs（去重）
	jobMap := make(map[string]*JobDefinition)
	for _, cfg := range configs {
		for i := range cfg.Workflows.Jobs {
			jobID := cfg.Workflows.Jobs[i].JobID
			if _, exists := jobMap[jobID]; !exists {
				jobMap[jobID] = &cfg.Workflows.Jobs[i]
			}
		}
	}
	for _, job := range jobMap {
		merged.Workflows.Jobs = append(merged.Workflows.Jobs, *job)
	}

	// 合并WorkflowDefinitions（去重）
	workflowMap := make(map[string]*WorkflowDefinition)
	for _, cfg := range configs {
		for i := range cfg.Workflows.Definitions {
			workflowID := cfg.Workflows.Definitions[i].WorkflowID
			if _, exists := workflowMap[workflowID]; !exists {
				workflowMap[workflowID] = &cfg.Workflows.Definitions[i]
			}
		}
	}
	for _, wf := range workflowMap {
		merged.Workflows.Definitions = append(merged.Workflows.Definitions, *wf)
	}

	return merged, nil
}

// replaceEnvVars 替换环境变量（如 ${ENV}, ${DB_PATH}）
func replaceEnvVars(data []byte) []byte {
	// 匹配 ${VAR_NAME} 或 ${VAR_NAME:default_value} 格式
	re := regexp.MustCompile(`\$\{([^}:]+)(?::([^}]*))?\}`)
	result := re.ReplaceAllFunc(data, func(match []byte) []byte {
		matches := re.FindSubmatch(match)
		if len(matches) < 2 {
			return match
		}

		varName := string(matches[1])
		defaultValue := ""
		if len(matches) > 2 {
			defaultValue = string(matches[2])
		}

		// 获取环境变量值
		value := os.Getenv(varName)
		if value == "" {
			value = defaultValue
		}

		// 特殊处理：${ENV} 如果没有设置，使用 "dev"
		if varName == "ENV" && value == "" && defaultValue == "" {
			value = "dev"
		}

		return []byte(value)
	})

	return result
}

// ParseDuration 解析时间字符串（支持 "30s", "1h", "2h30m" 等格式）
func ParseDuration(s string) (time.Duration, error) {
	if s == "" {
		return 0, nil
	}
	return time.ParseDuration(s)
}

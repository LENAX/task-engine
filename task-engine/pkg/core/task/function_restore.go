package task

import (
	"context"
	"fmt"
	"log"
)

// FunctionRestorer 函数恢复辅助结构体（对外导出）
// 封装函数恢复逻辑，简化使用方式
type FunctionRestorer struct {
	registry *FunctionRegistry
	funcMap  map[string]interface{}
}

// NewFunctionRestorer 创建函数恢复器（对外导出）
// registry: 函数注册中心
// funcMap: 函数名称 -> 函数实例的映射
func NewFunctionRestorer(registry *FunctionRegistry, funcMap map[string]interface{}) *FunctionRestorer {
	return &FunctionRestorer{
		registry: registry,
		funcMap:  funcMap,
	}
}

// Restore 执行函数恢复（对外导出）
// 从数据库恢复函数元数据，并通过funcMap恢复函数实例
func (fr *FunctionRestorer) Restore(ctx context.Context) error {
	if fr.registry == nil {
		return fmt.Errorf("函数注册中心未配置")
	}

	if fr.funcMap == nil {
		fr.funcMap = make(map[string]interface{})
	}

	// 调用FunctionRegistry的RestoreFunctions方法
	if err := fr.registry.RestoreFunctions(ctx, fr.funcMap); err != nil {
		return fmt.Errorf("恢复函数失败: %w", err)
	}

	// 统计恢复结果
	restoredCount := 0
	missingCount := 0

	// 从数据库加载所有函数元数据，统计恢复情况
	metas, err := fr.registry.jobFunctionRepo.ListAll(ctx)
	if err == nil {
		for _, meta := range metas {
			if _, exists := fr.funcMap[meta.Name]; exists {
				restoredCount++
			} else {
				missingCount++
			}
		}
	}

	log.Printf("✅ [函数恢复] 已恢复 %d 个函数实例，%d 个函数仅恢复元数据（函数实例不存在）",
		restoredCount, missingCount)

	return nil
}

// AddFunction 添加函数到映射表（对外导出）
// 用于动态构建funcMap
func (fr *FunctionRestorer) AddFunction(name string, fn interface{}) {
	if fr.funcMap == nil {
		fr.funcMap = make(map[string]interface{})
	}
	fr.funcMap[name] = fn
}

// GetFunctionMap 获取函数映射表（对外导出）
func (fr *FunctionRestorer) GetFunctionMap() map[string]interface{} {
	return fr.funcMap
}


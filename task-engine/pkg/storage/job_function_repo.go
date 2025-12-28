package storage

import (
	"context"
	"time"
)

// JobFunctionMeta Job函数元数据（对外导出）
type JobFunctionMeta struct {
	ID          string            // 函数唯一标识（UUID）
	Name        string            // 函数名称（唯一）
	Description string            // 函数描述
	ParamTypes  map[string]string // 参数类型映射（参数索引 -> 类型名，如 "arg0" -> "string"）
	ReturnType  string            // 返回值类型（如果有返回值）
	CreateTime  time.Time         // 创建时间
	UpdateTime  time.Time         // 更新时间
}

// JobFunctionRepository Job函数元数据存储接口（对外导出）
type JobFunctionRepository interface {
	// Save 保存函数元数据
	Save(ctx context.Context, meta *JobFunctionMeta) error
	// GetByName 根据函数名查询元数据
	GetByName(ctx context.Context, name string) (*JobFunctionMeta, error)
	// GetByID 根据ID查询元数据
	GetByID(ctx context.Context, id string) (*JobFunctionMeta, error)
	// ListAll 查询所有函数元数据
	ListAll(ctx context.Context) ([]*JobFunctionMeta, error)
	// Delete 删除函数元数据
	Delete(ctx context.Context, name string) error
}

package storage

import (
	"context"
	"time"
)

// TaskHandlerMeta Task Handler元数据（对外导出）
type TaskHandlerMeta struct {
	ID          string    // Handler唯一标识（UUID）
	Name        string    // Handler名称（唯一）
	Description string    // Handler描述
	CreateTime  time.Time // 创建时间
	UpdateTime  time.Time // 更新时间
}

// TaskHandlerRepository Task Handler元数据存储接口（对外导出）
type TaskHandlerRepository interface {
	// Save 保存Handler元数据
	Save(ctx context.Context, meta *TaskHandlerMeta) error
	// GetByName 根据Handler名称查询元数据
	GetByName(ctx context.Context, name string) (*TaskHandlerMeta, error)
	// GetByID 根据ID查询元数据
	GetByID(ctx context.Context, id string) (*TaskHandlerMeta, error)
	// ListAll 查询所有Handler元数据
	ListAll(ctx context.Context) ([]*TaskHandlerMeta, error)
	// Delete 删除Handler元数据
	Delete(ctx context.Context, name string) error
}

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

// TaskHandlerCRUDRepository Task Handler元数据通用CRUD接口（对外导出）
// 提供基础的增删改查操作
type TaskHandlerCRUDRepository interface {
	BaseRepository
	// Save 保存Handler元数据（创建或更新）
	Save(ctx context.Context, meta *TaskHandlerMeta) error
	// GetByID 根据ID查询Handler元数据
	GetByID(ctx context.Context, id string) (*TaskHandlerMeta, error)
	// Delete 删除Handler元数据（根据ID）
	Delete(ctx context.Context, id string) error
	// ListAll 查询所有Handler元数据
	ListAll(ctx context.Context) ([]*TaskHandlerMeta, error)
}

// TaskHandlerRepository Task Handler元数据业务存储接口（对外导出）
// 组合了通用CRUD接口，并添加了业务特定的方法
type TaskHandlerRepository interface {
	// 继承通用CRUD接口
	TaskHandlerCRUDRepository

	// GetByName 根据Handler名称查询元数据（业务接口）
	GetByName(ctx context.Context, name string) (*TaskHandlerMeta, error)
	// DeleteByName 根据名称删除Handler元数据（业务接口，Delete的别名，语义更清晰）
	DeleteByName(ctx context.Context, name string) error
}

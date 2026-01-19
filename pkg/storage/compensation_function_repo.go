package storage

import (
	"context"
	"time"
)

// CompensationFunctionMeta 补偿函数元数据（对外导出）
type CompensationFunctionMeta struct {
	ID          string    // 函数唯一标识（UUID）
	Name        string    // 函数名称（唯一）
	Description string    // 函数描述
	CreateTime  time.Time // 创建时间
	UpdateTime  time.Time // 更新时间
}

// CompensationFunctionCRUDRepository 补偿函数元数据通用CRUD接口（对外导出）
// 提供基础的增删改查操作
type CompensationFunctionCRUDRepository interface {
	BaseRepository
	// Save 保存函数元数据（创建或更新）
	Save(ctx context.Context, meta *CompensationFunctionMeta) error
	// GetByID 根据ID查询函数元数据
	GetByID(ctx context.Context, id string) (*CompensationFunctionMeta, error)
	// Delete 删除函数元数据（根据ID）
	Delete(ctx context.Context, id string) error
	// ListAll 查询所有函数元数据
	ListAll(ctx context.Context) ([]*CompensationFunctionMeta, error)
}

// CompensationFunctionRepository 补偿函数元数据业务存储接口（对外导出）
// 组合了通用CRUD接口，并添加了业务特定的方法
type CompensationFunctionRepository interface {
	// 继承通用CRUD接口
	CompensationFunctionCRUDRepository

	// GetByName 根据函数名查询元数据（业务接口）
	GetByName(ctx context.Context, name string) (*CompensationFunctionMeta, error)
	// DeleteByName 根据名称删除函数元数据（业务接口，Delete的别名，语义更清晰）
	DeleteByName(ctx context.Context, name string) error
}

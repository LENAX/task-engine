package storage

import (
	"context"
	"time"
)

// JobFunctionMeta Job函数元数据（对外导出）
type JobFunctionMeta struct {
	ID          string    // 函数唯一标识（UUID）
	Name        string    // 函数名称（唯一）
	Description string    // 函数描述
	CreateTime  time.Time // 创建时间
	UpdateTime  time.Time // 更新时间
	// 注意：不再存储ParamTypes和ReturnType，运行时从函数实例通过反射获取
}

// JobFunctionCRUDRepository Job函数元数据通用CRUD接口（对外导出）
// 提供基础的增删改查操作
type JobFunctionCRUDRepository interface {
	BaseRepository
	// Save 保存函数元数据（创建或更新）
	Save(ctx context.Context, meta *JobFunctionMeta) error
	// GetByID 根据ID查询函数元数据
	GetByID(ctx context.Context, id string) (*JobFunctionMeta, error)
	// Delete 删除函数元数据（根据ID）
	Delete(ctx context.Context, id string) error
	// ListAll 查询所有函数元数据
	ListAll(ctx context.Context) ([]*JobFunctionMeta, error)
}

// JobFunctionRepository Job函数元数据业务存储接口（对外导出）
// 组合了通用CRUD接口，并添加了业务特定的方法
type JobFunctionRepository interface {
	// 继承通用CRUD接口
	JobFunctionCRUDRepository

	// GetByName 根据函数名查询元数据（业务接口）
	GetByName(ctx context.Context, name string) (*JobFunctionMeta, error)
	// DeleteByName 根据名称删除函数元数据（业务接口，Delete的别名，语义更清晰）
	DeleteByName(ctx context.Context, name string) error
}

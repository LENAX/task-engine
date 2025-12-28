package dao

import (
	"time"
)

// JobFunctionDAO JobFunctionMeta表的数据访问对象（内部使用）
type JobFunctionDAO struct {
	ID          string    `db:"id"`
	Name        string    `db:"name"`
	Description string    `db:"description"`
	CodePath    string    `db:"code_path"`
	Hash        string    `db:"hash"`
	ParamTypes  string    `db:"param_types"` // JSON格式存储
	CreateTime  time.Time `db:"create_time"`
	UpdateTime  time.Time `db:"update_time"`
}


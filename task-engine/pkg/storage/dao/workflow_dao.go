package dao

import (
	"time"
)

// WorkflowDAO Workflow定义表的数据访问对象（内部使用）
type WorkflowDAO struct {
	ID           string    `db:"id"`
	Name         string    `db:"name"`
	Description  string    `db:"description"`
	Params       string    `db:"params"`       // JSON格式存储
	Dependencies string    `db:"dependencies"` // JSON格式存储
	CreateTime   time.Time `db:"create_time"`
	Status       string    `db:"status"` // ENABLED/DISABLED
}

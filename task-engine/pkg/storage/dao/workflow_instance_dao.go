package dao

import (
	"database/sql"
	"time"
)

// WorkflowInstanceDAO WorkflowInstance表的数据访问对象（内部使用）
type WorkflowInstanceDAO struct {
	ID           string         `db:"id"`
	WorkflowID   string         `db:"workflow_id"`
	Status       string         `db:"status"`
	StartTime    sql.NullTime   `db:"start_time"`
	EndTime      sql.NullTime   `db:"end_time"`
	Breakpoint   sql.NullString `db:"breakpoint"` // JSON格式存储
	ErrorMessage sql.NullString `db:"error_message"`
	CreateTime   time.Time      `db:"create_time"`
}

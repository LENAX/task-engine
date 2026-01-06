package dao

import (
	"database/sql"
	"time"
)

// TaskDAO TaskInstance表的数据访问对象（内部使用）
type TaskDAO struct {
	ID                 string         `db:"id"`
	Name               string         `db:"name"`
	WorkflowInstanceID string         `db:"workflow_instance_id"`
	JobFuncID            sql.NullString `db:"job_func_id"`
	JobFuncName          string         `db:"job_func_name"`
	CompensationFuncID   sql.NullString `db:"compensation_func_id"`
	CompensationFuncName string         `db:"compensation_func_name"`
	Params               string         `db:"params"` // JSON格式存储
	Status             string         `db:"status"`
	TimeoutSeconds     int            `db:"timeout_seconds"`
	RetryCount         int            `db:"retry_count"`
	StartTime          sql.NullTime   `db:"start_time"`
	EndTime            sql.NullTime   `db:"end_time"`
	ErrorMessage       sql.NullString `db:"error_msg"`
	CreateTime         time.Time      `db:"create_time"`
}

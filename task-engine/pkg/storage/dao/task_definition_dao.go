package dao

import (
	"database/sql"
	"time"
)

// TaskDefinitionDAO Task定义表的数据访问对象（内部使用）
// 用于持久化Workflow中的Task定义，与task_instance（运行时实例）区分
type TaskDefinitionDAO struct {
	ID                   string         `db:"id"`
	WorkflowID           string         `db:"workflow_id"`
	Name                 string         `db:"name"`
	Description          string         `db:"description"`
	JobFuncID            sql.NullString `db:"job_func_id"`
	JobFuncName          string         `db:"job_func_name"`
	CompensationFuncID   sql.NullString `db:"compensation_func_id"`
	CompensationFuncName string         `db:"compensation_func_name"`
	Params               string         `db:"params"`           // JSON格式存储
	TimeoutSeconds       int            `db:"timeout_seconds"`  // 超时时间（秒）
	RetryCount           int            `db:"retry_count"`      // 重试次数
	Dependencies         string         `db:"dependencies"`     // JSON数组，存储依赖的Task名称
	RequiredParams       string         `db:"required_params"`  // JSON数组，必需参数列表
	ResultMapping        string         `db:"result_mapping"`   // JSON对象，结果映射规则
	StatusHandlers       string         `db:"status_handlers"`  // JSON对象，状态处理器映射
	IsTemplate           bool           `db:"is_template"`      // 是否为模板任务
	CreateTime           time.Time      `db:"create_time"`
}


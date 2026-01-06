package types

import "time"

// Task 定义Task接口，作为公共类型避免循环依赖（对外导出）
// 注意：这个接口被多个包使用（workflow, task, executor, engine等），
// 放在公共包中可以避免循环依赖问题
type Task interface {
	GetID() string
	SetID(id string)
	GetName() string
	GetDescription() string
	SetDescription(description string)
	GetJobFuncName() string
	SetJobFuncName(jobFuncName string)
	GetParams() map[string]interface{}
	GetStatus() string
	SetStatus(status string)
	GetDependencies() []string
	SetDependencies(dependencies []string)
	IsSubTask() bool
	SetSubTask(isSubTask bool)
	IsTemplate() bool
	SetTemplate(isTemplate bool)
	GetJobFuncID() string
	SetJobFuncID(jobFuncID string)
	SetParam(key string, value interface{})
	GetParam(key string) (interface{}, bool)
	GetTimeoutSeconds() int
	SetTimeoutSeconds(timeoutSeconds int)
	GetCreateTime() time.Time
	SetCreateTime(createTime time.Time)
	GetStatusHandlers() map[string][]string
	SetStatusHandlers(statusHandlers map[string][]string)
	GetRetryCount() int
	SetRetryCount(retryCount int)
	GetRequiredParams() []string
	SetRequiredParams(requiredParams []string)
	GetResultMapping() map[string]string
	SetResultMapping(resultMapping map[string]string)
}

// TaskInfo 定义Task信息接口（用于DAG构建，避免循环依赖）
type TaskInfo interface {
	GetID() string
	GetName() string
}

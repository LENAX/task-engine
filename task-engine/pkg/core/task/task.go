package task

import (
	"time"

	"github.com/google/uuid"
)

type Task struct {
	ID          string
	Name        string
	Description string
	Params      map[string]string
	CreateTime  time.Time
	Status      string
}

// NewTask 创建Task实例（对外导出）
func NewTask(name, desc string) *Task {
	return &Task{
		ID:          uuid.NewString(),
		Name:        name,
		Description: desc,
		Status:      "ENABLED",
		CreateTime:  time.Now(),
	}
}

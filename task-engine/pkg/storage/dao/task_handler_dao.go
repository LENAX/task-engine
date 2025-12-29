package dao

import (
	"time"
)

// TaskHandlerDAO TaskHandlerMeta表的数据访问对象（内部使用）
type TaskHandlerDAO struct {
	ID          string    `db:"id"`
	Name        string    `db:"name"`
	Description string    `db:"description"`
	CreateTime  time.Time `db:"create_time"`
	UpdateTime  time.Time `db:"update_time"`
}

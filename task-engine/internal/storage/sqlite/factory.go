package sqlite

import (
	"fmt"

	"github.com/jmoiron/sqlx"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stevelan1995/task-engine/pkg/storage"
)

// Repositories 存储Repository集合（内部使用）
type Repositories struct {
	Workflow         storage.WorkflowRepository
	WorkflowInstance storage.WorkflowInstanceRepository
	Task             storage.TaskRepository
	JobFunction      storage.JobFunctionRepository
	TaskHandler      storage.TaskHandlerRepository
	db               *sqlx.DB
}

// NewRepositories 创建所有Repository实例（内部工厂方法，不导出）
// dsn: 数据库连接字符串（如"./data.db"）
func NewRepositories(dsn string) (*Repositories, error) {
	db, err := sqlx.Open("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("打开数据库失败: %w", err)
	}

	// 测试连接
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("数据库连接失败: %w", err)
	}

	workflowRepo, err := NewWorkflowRepoWithDB(db)
	if err != nil {
		return nil, fmt.Errorf("创建WorkflowRepository失败: %w", err)
	}

	repos := &Repositories{
		Workflow:         workflowRepo,
		WorkflowInstance: NewWorkflowInstanceRepo(db),
		Task:             NewTaskRepo(db),
		JobFunction:      NewJobFunctionRepo(db),
		TaskHandler:      NewTaskHandlerRepo(db),
		db:               db,
	}

	return repos, nil
}

// Close 关闭数据库连接（内部方法）
func (r *Repositories) Close() error {
	if r.db != nil {
		return r.db.Close()
	}
	return nil
}

// NewWorkflowRepoWithDB 使用已有的DB创建WorkflowRepository（内部方法）
func NewWorkflowRepoWithDB(db *sqlx.DB) (storage.WorkflowRepository, error) {
	repo := &workflowRepo{db: db}
	if err := repo.initSchema(); err != nil {
		return nil, fmt.Errorf("初始化表结构失败: %w", err)
	}
	return repo, nil
}

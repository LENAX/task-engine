package sqlite

import (
	"fmt"

	"github.com/jmoiron/sqlx"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stevelan1995/task-engine/pkg/storage"
	pkgsqlite "github.com/stevelan1995/task-engine/pkg/storage/sqlite"
)

// configureSQLite 配置SQLite数据库连接，启用WAL模式和其他优化设置
func configureSQLite(db *sqlx.DB) error {
	// 启用WAL模式：允许并发读写，显著提高并发性能
	_, err := db.Exec("PRAGMA journal_mode=WAL;")
	if err != nil {
		return fmt.Errorf("启用WAL模式失败: %w", err)
	}

	// 设置busy_timeout：当数据库被锁定时，等待最多30秒而不是立即失败
	// 这可以减少"database is locked"错误
	_, err = db.Exec("PRAGMA busy_timeout=30000;")
	if err != nil {
		return fmt.Errorf("设置busy_timeout失败: %w", err)
	}

	// 设置WAL自动检查点：当WAL文件达到1000页时自动执行检查点
	// 防止WAL文件无限增长
	_, err = db.Exec("PRAGMA wal_autocheckpoint=1000;")
	if err != nil {
		return fmt.Errorf("设置wal_autocheckpoint失败: %w", err)
	}

	// 设置同步模式为NORMAL（WAL模式下推荐）
	// WAL模式下，NORMAL同步模式已经足够安全，且性能更好
	_, err = db.Exec("PRAGMA synchronous=NORMAL;")
	if err != nil {
		return fmt.Errorf("设置synchronous失败: %w", err)
	}

	return nil
}

// Repositories 存储Repository集合（内部使用）
type Repositories struct {
	Workflow          storage.WorkflowRepository
	WorkflowInstance  storage.WorkflowInstanceRepository
	Task              storage.TaskRepository
	JobFunction       storage.JobFunctionRepository
	TaskHandler       storage.TaskHandlerRepository
	WorkflowAggregate storage.WorkflowAggregateRepository // 聚合根Repository
	db                *sqlx.DB
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

	// 配置SQLite：启用WAL模式和其他优化设置
	if err := configureSQLite(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("配置SQLite失败: %w", err)
	}

	workflowRepo, err := NewWorkflowRepoWithDB(db)
	if err != nil {
		return nil, fmt.Errorf("创建WorkflowRepository失败: %w", err)
	}

	workflowAggregateRepo, err := pkgsqlite.NewWorkflowAggregateRepo(db)
	if err != nil {
		return nil, fmt.Errorf("创建WorkflowAggregateRepository失败: %w", err)
	}

	repos := &Repositories{
		Workflow:          workflowRepo,
		WorkflowInstance:  NewWorkflowInstanceRepo(db),
		Task:              NewTaskRepo(db),
		JobFunction:       NewJobFunctionRepo(db),
		TaskHandler:       NewTaskHandlerRepo(db),
		WorkflowAggregate: workflowAggregateRepo,
		db:                db,
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

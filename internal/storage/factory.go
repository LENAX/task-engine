package storage

import (
	"fmt"

	"github.com/LENAX/task-engine/pkg/storage"
	"github.com/LENAX/task-engine/pkg/storage/mysql"
	"github.com/LENAX/task-engine/pkg/storage/postgres"
	pkgsqlite "github.com/LENAX/task-engine/pkg/storage/sqlite"
)

// DatabaseFactory 数据库工厂接口（内部使用）
type DatabaseFactory interface {
	// CreateWorkflowAggregateRepo 创建Workflow聚合根Repository
	CreateWorkflowAggregateRepo(dsn string) (storage.WorkflowAggregateRepository, error)
	// Close 关闭数据库连接
	Close() error
}

// Repositories 存储Repository集合（内部使用）
type Repositories struct {
	WorkflowAggregate storage.WorkflowAggregateRepository // 聚合根Repository（推荐使用）
	// 保留旧接口以兼容现有代码
	Workflow         storage.WorkflowRepository
	WorkflowInstance storage.WorkflowInstanceRepository
	Task             storage.TaskRepository
	JobFunction      storage.JobFunctionRepository
	TaskHandler      storage.TaskHandlerRepository
}

// NewDatabaseFactory 创建数据库工厂（内部方法）
// dbType: 数据库类型（sqlite/mysql/postgres）
// dsn: 数据库连接字符串
func NewDatabaseFactory(dbType, dsn string) (DatabaseFactory, error) {
	switch dbType {
	case "sqlite":
		return newSQLiteFactory(dsn)
	case "mysql":
		return newMySQLFactory(dsn)
	case "postgres", "postgresql":
		return newPostgresFactory(dsn)
	default:
		return nil, fmt.Errorf("unsupported database type: %s", dbType)
	}
}

// sqliteFactory SQLite数据库工厂（内部实现）
type sqliteFactory struct {
	aggregateRepo storage.WorkflowAggregateRepository
}

func newSQLiteFactory(dsn string) (*sqliteFactory, error) {
	repo, err := pkgsqlite.NewWorkflowAggregateRepoFromDSN(dsn)
	if err != nil {
		return nil, fmt.Errorf("create sqlite repository failed: %w", err)
	}
	return &sqliteFactory{
		aggregateRepo: repo,
	}, nil
}

func (f *sqliteFactory) CreateWorkflowAggregateRepo(dsn string) (storage.WorkflowAggregateRepository, error) {
	return f.aggregateRepo, nil
}

func (f *sqliteFactory) Close() error {
	if repo, ok := f.aggregateRepo.(interface{ Close() error }); ok {
		return repo.Close()
	}
	return nil
}

// mysqlFactory MySQL数据库工厂（内部实现）
type mysqlFactory struct {
	aggregateRepo storage.WorkflowAggregateRepository
}

func newMySQLFactory(dsn string) (*mysqlFactory, error) {
	repo, err := mysql.NewWorkflowAggregateRepoFromDSN(dsn)
	if err != nil {
		return nil, fmt.Errorf("create mysql repository failed: %w", err)
	}
	return &mysqlFactory{
		aggregateRepo: repo,
	}, nil
}

func (f *mysqlFactory) CreateWorkflowAggregateRepo(dsn string) (storage.WorkflowAggregateRepository, error) {
	return f.aggregateRepo, nil
}

func (f *mysqlFactory) Close() error {
	if repo, ok := f.aggregateRepo.(interface{ Close() error }); ok {
		return repo.Close()
	}
	return nil
}

// postgresFactory PostgreSQL数据库工厂（内部实现）
type postgresFactory struct {
	aggregateRepo storage.WorkflowAggregateRepository
}

func newPostgresFactory(dsn string) (*postgresFactory, error) {
	repo, err := postgres.NewWorkflowAggregateRepoFromDSN(dsn)
	if err != nil {
		return nil, fmt.Errorf("create postgres repository failed: %w", err)
	}
	return &postgresFactory{
		aggregateRepo: repo,
	}, nil
}

func (f *postgresFactory) CreateWorkflowAggregateRepo(dsn string) (storage.WorkflowAggregateRepository, error) {
	return f.aggregateRepo, nil
}

func (f *postgresFactory) Close() error {
	if repo, ok := f.aggregateRepo.(interface{ Close() error }); ok {
		return repo.Close()
	}
	return nil
}

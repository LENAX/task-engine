package integration

import (
	"context"
	"os"
	"testing"

	"github.com/stevelan1995/task-engine/internal/storage"
	"github.com/stevelan1995/task-engine/pkg/core/workflow"
)

// TestMultiDatabase_SQLite 测试SQLite数据库支持
func TestMultiDatabase_SQLite(t *testing.T) {
	tmpDir := t.TempDir()
	dsn := tmpDir + "/test_sqlite.db"

	// 创建SQLite数据库工厂
	factory, err := storage.NewDatabaseFactory("sqlite", dsn)
	if err != nil {
		t.Fatalf("创建SQLite数据库工厂失败: %v", err)
	}
	defer factory.Close()

	// 创建聚合Repository
	repo, err := factory.CreateWorkflowAggregateRepo(dsn)
	if err != nil {
		t.Fatalf("创建SQLite聚合Repository失败: %v", err)
	}

	// 测试保存和读取Workflow
	ctx := context.Background()
	wf := workflow.NewWorkflow("test-workflow", "测试Workflow")
	wf.SetCronExpr("0 0 0 * * *")
	wf.SetCronEnabled(true)

	// 保存Workflow
	err = repo.SaveWorkflow(ctx, wf)
	if err != nil {
		t.Fatalf("保存Workflow失败: %v", err)
	}

	// 读取Workflow
	loadedWf, err := repo.GetWorkflow(ctx, wf.GetID())
	if err != nil {
		t.Fatalf("读取Workflow失败: %v", err)
	}
	if loadedWf == nil {
		t.Fatal("读取的Workflow为空")
	}

	// 验证Cron字段
	if !loadedWf.IsCronEnabled() {
		t.Error("从数据库读取的Workflow应该启用Cron调度")
	}
	if loadedWf.GetCronExpr() != "0 0 0 * * *" {
		t.Errorf("从数据库读取的Cron表达式不匹配，期望: 0 0 0 * * *, 实际: %s", loadedWf.GetCronExpr())
	}

	t.Logf("✅ SQLite数据库测试通过: WorkflowID=%s, CronExpr=%s", loadedWf.GetID(), loadedWf.GetCronExpr())
}

// TestMultiDatabase_MySQL 测试MySQL数据库支持（需要MySQL环境）
func TestMultiDatabase_MySQL(t *testing.T) {
	// 检查是否有MySQL环境变量
	mysqlDSN := os.Getenv("MYSQL_DSN")
	if mysqlDSN == "" {
		t.Skip("跳过MySQL测试：未设置MYSQL_DSN环境变量")
	}

	// 创建MySQL数据库工厂
	factory, err := storage.NewDatabaseFactory("mysql", mysqlDSN)
	if err != nil {
		t.Fatalf("创建MySQL数据库工厂失败: %v", err)
	}
	defer factory.Close()

	// 创建聚合Repository
	repo, err := factory.CreateWorkflowAggregateRepo(mysqlDSN)
	if err != nil {
		t.Fatalf("创建MySQL聚合Repository失败: %v", err)
	}

	// 测试保存和读取Workflow
	ctx := context.Background()
	wf := workflow.NewWorkflow("test-mysql-workflow", "测试MySQL Workflow")
	wf.SetCronExpr("0 0 0 * * *")
	wf.SetCronEnabled(true)

	// 保存Workflow
	err = repo.SaveWorkflow(ctx, wf)
	if err != nil {
		t.Fatalf("保存Workflow失败: %v", err)
	}

	// 读取Workflow
	loadedWf, err := repo.GetWorkflow(ctx, wf.GetID())
	if err != nil {
		t.Fatalf("读取Workflow失败: %v", err)
	}
	if loadedWf == nil {
		t.Fatal("读取的Workflow为空")
	}

	// 验证Cron字段
	if !loadedWf.IsCronEnabled() {
		t.Error("从数据库读取的Workflow应该启用Cron调度")
	}
	if loadedWf.GetCronExpr() != "0 0 0 * * *" {
		t.Errorf("从数据库读取的Cron表达式不匹配，期望: 0 0 0 * * *, 实际: %s", loadedWf.GetCronExpr())
	}

	// 清理：删除测试数据
	_ = repo.DeleteWorkflow(ctx, wf.GetID())

	t.Logf("✅ MySQL数据库测试通过: WorkflowID=%s, CronExpr=%s", loadedWf.GetID(), loadedWf.GetCronExpr())
}

// TestMultiDatabase_PostgreSQL 测试PostgreSQL数据库支持（需要PostgreSQL环境）
func TestMultiDatabase_PostgreSQL(t *testing.T) {
	// 检查是否有PostgreSQL环境变量
	postgresDSN := os.Getenv("POSTGRES_DSN")
	if postgresDSN == "" {
		t.Skip("跳过PostgreSQL测试：未设置POSTGRES_DSN环境变量")
	}

	// 创建PostgreSQL数据库工厂
	factory, err := storage.NewDatabaseFactory("postgres", postgresDSN)
	if err != nil {
		t.Fatalf("创建PostgreSQL数据库工厂失败: %v", err)
	}
	defer factory.Close()

	// 创建聚合Repository
	repo, err := factory.CreateWorkflowAggregateRepo(postgresDSN)
	if err != nil {
		t.Fatalf("创建PostgreSQL聚合Repository失败: %v", err)
	}

	// 测试保存和读取Workflow
	ctx := context.Background()
	wf := workflow.NewWorkflow("test-postgres-workflow", "测试PostgreSQL Workflow")
	wf.SetCronExpr("0 0 0 * * *")
	wf.SetCronEnabled(true)

	// 保存Workflow
	err = repo.SaveWorkflow(ctx, wf)
	if err != nil {
		t.Fatalf("保存Workflow失败: %v", err)
	}

	// 读取Workflow
	loadedWf, err := repo.GetWorkflow(ctx, wf.GetID())
	if err != nil {
		t.Fatalf("读取Workflow失败: %v", err)
	}
	if loadedWf == nil {
		t.Fatal("读取的Workflow为空")
	}

	// 验证Cron字段
	if !loadedWf.IsCronEnabled() {
		t.Error("从数据库读取的Workflow应该启用Cron调度")
	}
	if loadedWf.GetCronExpr() != "0 0 0 * * *" {
		t.Errorf("从数据库读取的Cron表达式不匹配，期望: 0 0 0 * * *, 实际: %s", loadedWf.GetCronExpr())
	}

	// 清理：删除测试数据
	_ = repo.DeleteWorkflow(ctx, wf.GetID())

	t.Logf("✅ PostgreSQL数据库测试通过: WorkflowID=%s, CronExpr=%s", loadedWf.GetID(), loadedWf.GetCronExpr())
}

// TestMultiDatabase_UnsupportedType 测试不支持的数据库类型
func TestMultiDatabase_UnsupportedType(t *testing.T) {
	_, err := storage.NewDatabaseFactory("oracle", "test-dsn")
	if err == nil {
		t.Error("应该返回错误：不支持的数据库类型")
	}
	if err != nil && err.Error() != "unsupported database type: oracle" {
		t.Errorf("错误信息不匹配，期望包含'unsupported database type: oracle'，实际: %v", err)
	}
}

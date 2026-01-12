package postgres

import (
	"fmt"
	"strings"

	"github.com/LENAX/task-engine/pkg/storage"
)

// PostgresDialect PostgreSQL方言实现（对外导出）
type PostgresDialect struct{}

// NewPostgresDialect 创建PostgreSQL方言实例
func NewPostgresDialect() *PostgresDialect {
	return &PostgresDialect{}
}

// Name 返回方言名称
func (d *PostgresDialect) Name() string {
	return "postgres"
}

// Placeholder 返回占位符（PostgreSQL使用$1, $2, ...）
// 注意：sqlx的NamedExec会自动处理:name形式的占位符
func (d *PostgresDialect) Placeholder(index int) string {
	return fmt.Sprintf("$%d", index)
}

// UpsertSQL 返回PostgreSQL的UPSERT语句（使用ON CONFLICT DO UPDATE）
func (d *PostgresDialect) UpsertSQL(tableName string, columns []string, conflictColumn string, updateColumns []string) string {
	namedPlaceholders := make([]string, len(columns))
	for i, col := range columns {
		namedPlaceholders[i] = ":" + col
	}

	// 构建ON CONFLICT DO UPDATE子句
	updateParts := make([]string, len(updateColumns))
	for i, col := range updateColumns {
		updateParts[i] = fmt.Sprintf("%s = EXCLUDED.%s", col, col)
	}

	return fmt.Sprintf(
		"INSERT INTO %s (%s) VALUES (%s) ON CONFLICT (%s) DO UPDATE SET %s",
		tableName,
		strings.Join(columns, ", "),
		strings.Join(namedPlaceholders, ", "),
		conflictColumn,
		strings.Join(updateParts, ", "),
	)
}

// CreateTableSQL 转换DDL为PostgreSQL兼容格式
func (d *PostgresDialect) CreateTableSQL(schema string) string {
	result := schema

	// 替换DATETIME为TIMESTAMP
	result = strings.ReplaceAll(result, "DATETIME", "TIMESTAMP")

	// 替换REAL为DOUBLE PRECISION
	result = strings.ReplaceAll(result, "REAL NOT NULL", "DOUBLE PRECISION NOT NULL")
	result = strings.ReplaceAll(result, "REAL DEFAULT", "DOUBLE PRECISION DEFAULT")

	// 替换INTEGER PRIMARY KEY为SERIAL PRIMARY KEY（自增）
	result = strings.ReplaceAll(result, "INTEGER PRIMARY KEY AUTOINCREMENT", "SERIAL PRIMARY KEY")

	// 替换布尔INTEGER为BOOLEAN
	// 注意：这需要更精确的匹配，这里简化处理
	result = strings.ReplaceAll(result, "INTEGER NOT NULL DEFAULT 0", "BOOLEAN NOT NULL DEFAULT FALSE")
	result = strings.ReplaceAll(result, "INTEGER DEFAULT 0", "BOOLEAN DEFAULT FALSE")

	// 移除SQLite特有的ON DELETE CASCADE（PostgreSQL也支持，但语法可能略有不同）
	// PostgreSQL支持相同语法，无需修改

	return result
}

// ConfigureDB 返回PostgreSQL配置SQL
func (d *PostgresDialect) ConfigureDB() []string {
	return []string{
		// PostgreSQL通常不需要特殊配置
		// 可以根据需要添加如设置时区等
		"SET timezone = 'UTC';",
	}
}

// AutoIncrementKeyword 返回PostgreSQL自增关键字
func (d *PostgresDialect) AutoIncrementKeyword() string {
	return "SERIAL PRIMARY KEY"
}

// BooleanType 返回PostgreSQL布尔类型
func (d *PostgresDialect) BooleanType() string {
	return "BOOLEAN"
}

// TextType 返回PostgreSQL文本类型
func (d *PostgresDialect) TextType() string {
	return "TEXT"
}

// TimestampType 返回PostgreSQL时间戳类型
func (d *PostgresDialect) TimestampType() string {
	return "TIMESTAMP"
}

// FloatType 返回PostgreSQL浮点类型
func (d *PostgresDialect) FloatType() string {
	return "DOUBLE PRECISION"
}

// 确保实现接口
var _ storage.Dialect = (*PostgresDialect)(nil)

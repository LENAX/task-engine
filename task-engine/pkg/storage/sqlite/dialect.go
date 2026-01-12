package sqlite

import (
	"fmt"
	"strings"

	"github.com/LENAX/task-engine/pkg/storage"
)

// SQLiteDialect SQLite方言实现（对外导出）
type SQLiteDialect struct{}

// NewSQLiteDialect 创建SQLite方言实例
func NewSQLiteDialect() *SQLiteDialect {
	return &SQLiteDialect{}
}

// Name 返回方言名称
func (d *SQLiteDialect) Name() string {
	return "sqlite"
}

// Placeholder 返回占位符（SQLite使用?）
func (d *SQLiteDialect) Placeholder(index int) string {
	return "?"
}

// UpsertSQL 返回SQLite的UPSERT语句
func (d *SQLiteDialect) UpsertSQL(tableName string, columns []string, conflictColumn string, updateColumns []string) string {
	// SQLite 3.24+ 支持 ON CONFLICT
	// 但为了兼容性，使用 INSERT OR REPLACE
	placeholders := make([]string, len(columns))
	namedPlaceholders := make([]string, len(columns))
	for i, col := range columns {
		placeholders[i] = "?"
		namedPlaceholders[i] = ":" + col
	}

	return fmt.Sprintf(
		"INSERT OR REPLACE INTO %s (%s) VALUES (%s)",
		tableName,
		strings.Join(columns, ", "),
		strings.Join(namedPlaceholders, ", "),
	)
}

// CreateTableSQL 返回创建表的DDL（SQLite原样返回）
func (d *SQLiteDialect) CreateTableSQL(schema string) string {
	return schema
}

// ConfigureDB 返回SQLite配置SQL
func (d *SQLiteDialect) ConfigureDB() []string {
	return []string{
		"PRAGMA journal_mode=WAL;",
		"PRAGMA busy_timeout=30000;",
		"PRAGMA wal_autocheckpoint=1000;",
		"PRAGMA synchronous=NORMAL;",
	}
}

// AutoIncrementKeyword 返回SQLite自增关键字
func (d *SQLiteDialect) AutoIncrementKeyword() string {
	return "INTEGER PRIMARY KEY AUTOINCREMENT"
}

// BooleanType 返回SQLite布尔类型
func (d *SQLiteDialect) BooleanType() string {
	return "INTEGER"
}

// TextType 返回SQLite文本类型
func (d *SQLiteDialect) TextType() string {
	return "TEXT"
}

// TimestampType 返回SQLite时间戳类型
func (d *SQLiteDialect) TimestampType() string {
	return "DATETIME"
}

// FloatType 返回SQLite浮点类型
func (d *SQLiteDialect) FloatType() string {
	return "REAL"
}

// 确保实现接口
var _ storage.Dialect = (*SQLiteDialect)(nil)


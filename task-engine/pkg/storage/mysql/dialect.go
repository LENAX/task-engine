package mysql

import (
	"fmt"
	"strings"

	"github.com/LENAX/task-engine/pkg/storage"
)

// MySQLDialect MySQL方言实现（对外导出）
type MySQLDialect struct{}

// NewMySQLDialect 创建MySQL方言实例
func NewMySQLDialect() *MySQLDialect {
	return &MySQLDialect{}
}

// Name 返回方言名称
func (d *MySQLDialect) Name() string {
	return "mysql"
}

// Placeholder 返回占位符（MySQL使用?）
func (d *MySQLDialect) Placeholder(index int) string {
	return "?"
}

// UpsertSQL 返回MySQL的UPSERT语句（使用ON DUPLICATE KEY UPDATE）
func (d *MySQLDialect) UpsertSQL(tableName string, columns []string, conflictColumn string, updateColumns []string) string {
	namedPlaceholders := make([]string, len(columns))
	for i, col := range columns {
		namedPlaceholders[i] = ":" + col
	}

	// 构建ON DUPLICATE KEY UPDATE子句
	updateParts := make([]string, len(updateColumns))
	for i, col := range updateColumns {
		updateParts[i] = fmt.Sprintf("%s = VALUES(%s)", col, col)
	}

	return fmt.Sprintf(
		"INSERT INTO %s (%s) VALUES (%s) ON DUPLICATE KEY UPDATE %s",
		tableName,
		strings.Join(columns, ", "),
		strings.Join(namedPlaceholders, ", "),
		strings.Join(updateParts, ", "),
	)
}

// CreateTableSQL 转换DDL为MySQL兼容格式
func (d *MySQLDialect) CreateTableSQL(schema string) string {
	// 替换SQLite特有语法为MySQL兼容语法
	result := schema

	// TEXT类型在MySQL中保持不变
	// DATETIME类型在MySQL中保持不变

	// 替换REAL为DOUBLE
	result = strings.ReplaceAll(result, "REAL NOT NULL", "DOUBLE NOT NULL")
	result = strings.ReplaceAll(result, "REAL DEFAULT", "DOUBLE DEFAULT")

	// 替换INTEGER为INT（用于布尔字段）
	// 注意：不能简单替换所有INTEGER，因为有些确实是整数

	// 替换AUTOINCREMENT为AUTO_INCREMENT
	result = strings.ReplaceAll(result, "AUTOINCREMENT", "AUTO_INCREMENT")

	// 添加引擎声明
	if !strings.Contains(result, "ENGINE=") && strings.Contains(result, "CREATE TABLE") {
		result = strings.TrimRight(result, ";") + " ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;"
	}

	return result
}

// ConfigureDB 返回MySQL配置SQL
func (d *MySQLDialect) ConfigureDB() []string {
	return []string{
		"SET SESSION sql_mode='STRICT_TRANS_TABLES,NO_ZERO_IN_DATE,NO_ZERO_DATE,ERROR_FOR_DIVISION_BY_ZERO,NO_ENGINE_SUBSTITUTION';",
	}
}

// AutoIncrementKeyword 返回MySQL自增关键字
func (d *MySQLDialect) AutoIncrementKeyword() string {
	return "INT PRIMARY KEY AUTO_INCREMENT"
}

// BooleanType 返回MySQL布尔类型
func (d *MySQLDialect) BooleanType() string {
	return "TINYINT(1)"
}

// TextType 返回MySQL文本类型
func (d *MySQLDialect) TextType() string {
	return "TEXT"
}

// TimestampType 返回MySQL时间戳类型
func (d *MySQLDialect) TimestampType() string {
	return "DATETIME"
}

// FloatType 返回MySQL浮点类型
func (d *MySQLDialect) FloatType() string {
	return "DOUBLE"
}

// 确保实现接口
var _ storage.Dialect = (*MySQLDialect)(nil)

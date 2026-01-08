package output

import (
	"fmt"
	"strings"

	"github.com/fatih/color"
)

// Table 简单表格输出
type Table struct {
	headers []string
	rows    [][]string
	widths  []int
}

// NewTable 创建表格
func NewTable(headers []string) *Table {
	widths := make([]int, len(headers))
	for i, h := range headers {
		widths[i] = len(h)
	}
	return &Table{
		headers: headers,
		rows:    make([][]string, 0),
		widths:  widths,
	}
}

// AddRow 添加行
func (t *Table) AddRow(row []string) {
	// 更新列宽
	for i, cell := range row {
		if i < len(t.widths) && len(cell) > t.widths[i] {
			t.widths[i] = len(cell)
		}
	}
	t.rows = append(t.rows, row)
}

// Render 渲染表格
func (t *Table) Render() {
	// 打印表头
	headerColor := color.New(color.FgCyan, color.Bold)
	for i, h := range t.headers {
		headerColor.Printf("%-*s  ", t.widths[i], h)
	}
	fmt.Println()

	// 打印分隔线
	for i := range t.headers {
		fmt.Print(strings.Repeat("-", t.widths[i]))
		fmt.Print("  ")
	}
	fmt.Println()

	// 打印数据行
	for _, row := range t.rows {
		for i, cell := range row {
			if i < len(t.widths) {
				fmt.Printf("%-*s  ", t.widths[i], cell)
			}
		}
		fmt.Println()
	}
}

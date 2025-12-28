package main

import (
	"fmt"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("用法:")
		fmt.Println("  go run . save       - 保存任务和函数到数据库（旧API）")
		fmt.Println("  go run . load       - 从数据库加载并执行任务（旧API）")
		fmt.Println("  go run . save2      - 保存任务和函数到数据库（新API）")
		fmt.Println("  go run . load2     - 从数据库加载并执行任务（新API）")
		fmt.Println("  go run . save <db_path>  - 指定数据库路径保存")
		fmt.Println("  go run . load <db_path>  - 指定数据库路径加载")
		os.Exit(1)
	}

	command := os.Args[1]
	switch command {
	case "save":
		saveTask()
	case "load":
		loadAndRun()
	case "save2":
		saveTaskV2()
	case "load2":
		loadAndRunV2()
	default:
		fmt.Printf("未知命令: %s\n", command)
		fmt.Println("可用命令: save, load, save2, load2")
		os.Exit(1)
	}
}

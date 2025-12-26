package main

import (
	"log"

	// 引用task-engine的核心包（模块名需和task-engine的go.mod一致）
	"github.com/stevelan1995/task-engine/pkg/config"
	"github.com/stevelan1995/task-engine/pkg/engine"
)

func main() {
	log.Println("===== 测试引用 quant-task-engine（加载测试配置） =====")

	// 🌟 关键：获取测试项目的配置文件绝对路径（避免相对路径问题）
	// 方式1：相对路径（简单，推荐）
	configPath := "config/engine.yaml"

	// 方式2：绝对路径（更稳定，适配任意运行目录）
	// _, currentFile, _, _ := runtime.Caller(0)
	// projectRoot := filepath.Dir(currentFile)
	// configPath = filepath.Join(projectRoot, "config/engine.yaml")

	// 1. 加载测试项目的自定义配置文件
	cfg, err := config.Load(configPath)
	if err != nil {
		log.Fatal("❌ 加载测试配置失败：", err)
	}
	// 验证配置是否加载成功（打印测试项目的自定义配置）
	log.Printf("✅ 加载测试配置成功 | 模式：%s | 端口：%d | 数据库路径：%s",
		cfg.Mode, cfg.HTTPPort, cfg.Database.Path)

	// 2. 用测试配置初始化task-engine引擎
	eng, err := engine.NewEngine(cfg)
	if err != nil {
		log.Fatal("❌ 初始化引擎失败：", err)
	}
	log.Println("✅ 引擎初始化成功（使用测试配置）")

	// 3. （可选）启动引擎（验证端口/模式是否为测试配置）
	// if err := eng.Start(); err != nil {
	// 	log.Fatal("❌ 启动引擎失败：", err)
	// }
	// log.Printf("✅ 引擎启动成功 | 测试端口：http://localhost:%d", cfg.HTTPPort)

	// 阻塞运行（按任意键停止）
	log.Println("\n按回车键停止引擎...")
	// var input string
	// log.Scanln(&input)

	// 4. 停止引擎
	eng.Stop()
	log.Println("✅ 引擎已停止，测试完成")
}

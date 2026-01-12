package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/LENAX/task-engine/pkg/core/builder"
	"github.com/LENAX/task-engine/pkg/core/engine"
	"github.com/LENAX/task-engine/pkg/plugin"
)

func main() {
	// 1. 创建邮件插件
	emailPlugin := plugin.NewEmailPlugin()

	// 2. 初始化邮件插件（配置SMTP信息）
	// 注意：这里使用示例配置，实际使用时请替换为真实的SMTP服务器信息
	emailParams := map[string]string{
		"smtp_host": "smtp.example.com",      // SMTP服务器地址
		"smtp_port": "587",                   // SMTP端口（587为TLS，465为SSL，25为普通）
		"username":  "your_username",         // SMTP用户名（如果需要认证）
		"password":  "your_password",         // SMTP密码（如果需要认证）
		"from":      "sender@example.com",    // 发件人地址
		"to":        "recipient@example.com", // 收件人地址（多个用逗号分隔）
	}
	if err := emailPlugin.Init(emailParams); err != nil {
		log.Fatalf("初始化邮件插件失败: %v", err)
	}

	// 3. 创建Engine并注册插件
	configPath := "config.yaml" // 请确保配置文件存在
	eng, err := engine.NewEngineBuilder(configPath).
		WithPlugin(emailPlugin).
		// 绑定插件到Workflow事件
		WithPluginBinding(plugin.PluginBinding{
			PluginName: "email",
			Event:      plugin.EventWorkflowStarted,
		}).
		WithPluginBinding(plugin.PluginBinding{
			PluginName: "email",
			Event:      plugin.EventWorkflowCompleted,
		}).
		WithPluginBinding(plugin.PluginBinding{
			PluginName: "email",
			Event:      plugin.EventWorkflowFailed,
		}).
		// 绑定插件到Task事件
		WithPluginBinding(plugin.PluginBinding{
			PluginName: "email",
			Event:      plugin.EventTaskFailed,
		}).
		Build()
	if err != nil {
		log.Fatalf("构建Engine失败: %v", err)
	}

	// 4. 启动Engine
	ctx := context.Background()
	if err := eng.Start(ctx); err != nil {
		log.Fatalf("启动Engine失败: %v", err)
	}
	defer eng.Stop()

	// 5. 注册Job函数
	jobFunc := func(ctx context.Context) error {
		log.Println("执行任务...")
		time.Sleep(1 * time.Second)
		return nil
	}
	_, err = eng.GetRegistry().Register(ctx, "example_job", jobFunc, "示例Job函数")
	if err != nil {
		log.Fatalf("注册Job函数失败: %v", err)
	}

	// 6. 创建Workflow
	wfBuilder := builder.NewWorkflowBuilder("example_workflow", "示例Workflow")
	taskBuilder := builder.NewTaskBuilder("task1", "任务1", eng.GetRegistry())
	task1, err := taskBuilder.WithJobFunction("example_job", nil).Build()
	if err != nil {
		log.Fatalf("创建Task失败: %v", err)
	}
	wf, err := wfBuilder.WithTask(task1).Build()
	if err != nil {
		log.Fatalf("创建Workflow失败: %v", err)
	}

	// 7. 提交Workflow
	ctrl, err := eng.SubmitWorkflow(ctx, wf)
	if err != nil {
		log.Fatalf("提交Workflow失败: %v", err)
	}
	fmt.Printf("Workflow已提交，Instance ID: %s\n", ctrl.InstanceID())

	// 8. 等待Workflow完成
	time.Sleep(3 * time.Second)

	// 9. 查询Workflow状态
	status, err := ctrl.GetStatus()
	if err != nil {
		log.Fatalf("获取Workflow状态失败: %v", err)
	}
	fmt.Printf("Workflow状态: %s\n", status)

	fmt.Println("示例完成！邮件插件已触发，请检查收件箱。")
}

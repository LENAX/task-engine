package plugin

import (
	"crypto/tls"
	"fmt"
	"log"
	"net/smtp"
	"strings"
)

// EmailPlugin 邮件发送插件（对外导出）
type EmailPlugin struct {
	name     string
	smtpHost string
	smtpPort int
	username string
	password string
	from     string
	to       []string
	enabled  bool
}

// NewEmailPlugin 创建邮件发送插件（对外导出）
func NewEmailPlugin() Plugin {
	return &EmailPlugin{
		name:    "email",
		enabled: false,
	}
}

// Name 插件名称（实现Plugin接口）
func (e *EmailPlugin) Name() string {
	return e.name
}

// Init 初始化插件（实现Plugin接口）
func (e *EmailPlugin) Init(params map[string]string) error {
	// 读取SMTP配置
	e.smtpHost = params["smtp_host"]
	if e.smtpHost == "" {
		return fmt.Errorf("smtp_host参数不能为空")
	}

	// SMTP端口（默认25）
	e.smtpPort = 25
	if portStr := params["smtp_port"]; portStr != "" {
		if _, err := fmt.Sscanf(portStr, "%d", &e.smtpPort); err != nil {
			return fmt.Errorf("smtp_port参数格式错误: %w", err)
		}
	}

	// 用户名和密码（可选，用于认证）
	e.username = params["username"]
	e.password = params["password"]

	// 发件人地址
	e.from = params["from"]
	if e.from == "" {
		return fmt.Errorf("from参数不能为空")
	}

	// 收件人地址（多个用逗号分隔）
	toStr := params["to"]
	if toStr == "" {
		return fmt.Errorf("to参数不能为空")
	}
	e.to = strings.Split(toStr, ",")
	for i := range e.to {
		e.to[i] = strings.TrimSpace(e.to[i])
	}

	e.enabled = true
	log.Printf("✅ [EmailPlugin] 初始化完成: SMTP=%s:%d, From=%s, To=%v", e.smtpHost, e.smtpPort, e.from, e.to)
	return nil
}

// Execute 执行邮件发送（实现Plugin接口）
func (e *EmailPlugin) Execute(data interface{}) error {
	if !e.enabled {
		return fmt.Errorf("邮件插件未初始化")
	}

	pluginData, ok := data.(PluginData)
	if !ok {
		return fmt.Errorf("插件数据类型错误")
	}

	// 构建邮件内容
	subject := e.buildSubject(pluginData)
	body := e.buildBody(pluginData)

	// 发送邮件
	if err := e.sendEmail(subject, body); err != nil {
		log.Printf("❌ [EmailPlugin] 发送邮件失败: %v", err)
		return err
	}

	log.Printf("✅ [EmailPlugin] 邮件发送成功: Event=%s, Subject=%s", pluginData.Event, subject)
	return nil
}

// buildSubject 构建邮件主题
func (e *EmailPlugin) buildSubject(data PluginData) string {
	switch data.Event {
	case EventWorkflowStarted:
		return fmt.Sprintf("[Workflow启动] %s - %s", data.WorkflowID, data.InstanceID)
	case EventWorkflowCompleted:
		return fmt.Sprintf("[Workflow完成] %s - %s", data.WorkflowID, data.InstanceID)
	case EventWorkflowFailed:
		return fmt.Sprintf("[Workflow失败] %s - %s", data.WorkflowID, data.InstanceID)
	case EventWorkflowPaused:
		return fmt.Sprintf("[Workflow暂停] %s - %s", data.WorkflowID, data.InstanceID)
	case EventWorkflowResumed:
		return fmt.Sprintf("[Workflow恢复] %s - %s", data.WorkflowID, data.InstanceID)
	case EventWorkflowTerminated:
		return fmt.Sprintf("[Workflow终止] %s - %s", data.WorkflowID, data.InstanceID)
	case EventTaskSuccess:
		return fmt.Sprintf("[Task成功] %s - %s", data.TaskName, data.TaskID)
	case EventTaskFailed:
		return fmt.Sprintf("[Task失败] %s - %s", data.TaskName, data.TaskID)
	case EventTaskTimeout:
		return fmt.Sprintf("[Task超时] %s - %s", data.TaskName, data.TaskID)
	default:
		return fmt.Sprintf("[系统通知] %s", data.Event)
	}
}

// buildBody 构建邮件正文
func (e *EmailPlugin) buildBody(data PluginData) string {
	var body strings.Builder
	body.WriteString(fmt.Sprintf("事件类型: %s\n", data.Event))
	body.WriteString(fmt.Sprintf("状态: %s\n", data.Status))
	
	if data.WorkflowID != "" {
		body.WriteString(fmt.Sprintf("Workflow ID: %s\n", data.WorkflowID))
	}
	if data.InstanceID != "" {
		body.WriteString(fmt.Sprintf("Instance ID: %s\n", data.InstanceID))
	}
	if data.TaskID != "" {
		body.WriteString(fmt.Sprintf("Task ID: %s\n", data.TaskID))
	}
	if data.TaskName != "" {
		body.WriteString(fmt.Sprintf("Task名称: %s\n", data.TaskName))
	}
	if data.Error != nil {
		body.WriteString(fmt.Sprintf("错误信息: %s\n", data.Error.Error()))
	}
	if len(data.Data) > 0 {
		body.WriteString("\n详细信息:\n")
		for k, v := range data.Data {
			body.WriteString(fmt.Sprintf("  %s: %v\n", k, v))
		}
	}
	return body.String()
}

// sendEmail 发送邮件
func (e *EmailPlugin) sendEmail(subject, body string) error {
	// 构建邮件消息
	message := e.buildMessage(subject, body)

	// SMTP服务器地址
	addr := fmt.Sprintf("%s:%d", e.smtpHost, e.smtpPort)

	// 如果配置了用户名和密码，使用认证
	if e.username != "" && e.password != "" {
		auth := smtp.PlainAuth("", e.username, e.password, e.smtpHost)
		
		// 对于TLS，需要先建立TLS连接
		if e.smtpPort == 465 {
			return e.sendEmailTLS(addr, auth, message)
		}
		
		return smtp.SendMail(addr, auth, e.from, e.to, []byte(message))
	}

	// 无认证发送（不推荐，仅用于测试）
	return smtp.SendMail(addr, nil, e.from, e.to, []byte(message))
}

// sendEmailTLS 通过TLS发送邮件（用于465端口）
func (e *EmailPlugin) sendEmailTLS(addr string, auth smtp.Auth, message string) error {
	// 建立TLS连接
	conn, err := tls.Dial("tcp", addr, &tls.Config{
		ServerName: e.smtpHost,
	})
	if err != nil {
		return fmt.Errorf("TLS连接失败: %w", err)
	}
	defer conn.Close()

	client, err := smtp.NewClient(conn, e.smtpHost)
	if err != nil {
		return fmt.Errorf("创建SMTP客户端失败: %w", err)
	}
	defer client.Close()

	// 认证
	if err := client.Auth(auth); err != nil {
		return fmt.Errorf("SMTP认证失败: %w", err)
	}

	// 设置发件人
	if err := client.Mail(e.from); err != nil {
		return fmt.Errorf("设置发件人失败: %w", err)
	}

	// 设置收件人
	for _, to := range e.to {
		if err := client.Rcpt(to); err != nil {
			return fmt.Errorf("设置收件人失败: %w", err)
		}
	}

	// 发送邮件内容
	writer, err := client.Data()
	if err != nil {
		return fmt.Errorf("获取数据写入器失败: %w", err)
	}
	if _, err := writer.Write([]byte(message)); err != nil {
		return fmt.Errorf("写入邮件内容失败: %w", err)
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("关闭数据写入器失败: %w", err)
	}

	return client.Quit()
}

// buildMessage 构建邮件消息
func (e *EmailPlugin) buildMessage(subject, body string) string {
	var message strings.Builder
	message.WriteString(fmt.Sprintf("From: %s\r\n", e.from))
	message.WriteString(fmt.Sprintf("To: %s\r\n", strings.Join(e.to, ", ")))
	message.WriteString(fmt.Sprintf("Subject: %s\r\n", subject))
	message.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	message.WriteString("\r\n")
	message.WriteString(body)
	return message.String()
}


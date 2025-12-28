package main

import (
	"context"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"os"
	"time"
)

// AddNumbers 示例函数：两个数字相加（模拟计算IO）
func AddNumbers(ctx context.Context, a int, b int) (int, error) {
	// 模拟计算延迟
	time.Sleep(100 * time.Millisecond)

	result := a + b
	fmt.Printf("[AddNumbers] 开始计算: %d + %d\n", a, b)

	// 模拟写入操作
	fmt.Printf("[AddNumbers] 计算结果: %d\n", result)

	return result, nil
}

// GreetUser 示例函数：问候用户（模拟网络IO）
func GreetUser(ctx context.Context, name string) (string, error) {
	fmt.Printf("[GreetUser] 开始处理用户: %s\n", name)

	// 模拟网络请求延迟
	time.Sleep(200 * time.Millisecond)

	// 模拟HTTP请求（不实际发送）
	client := &http.Client{Timeout: 1 * time.Second}
	req, _ := http.NewRequestWithContext(ctx, "GET", "https://httpbin.org/delay/0", nil)
	resp, err := client.Do(req)
	if err != nil {
		// 网络错误时使用本地处理
		fmt.Printf("[GreetUser] 网络请求失败，使用本地处理: %v\n", err)
	} else {
		resp.Body.Close()
		fmt.Printf("[GreetUser] 网络请求完成，状态码: %d\n", resp.StatusCode)
	}

	greeting := fmt.Sprintf("Hello, %s!", name)
	fmt.Printf("[GreetUser] 生成问候: %s\n", greeting)
	return greeting, nil
}

// ProcessData 示例函数：处理数据（模拟文件IO）
func ProcessData(ctx context.Context, data string, count int) (string, error) {
	fmt.Printf("[ProcessData] 开始处理数据: %s (count: %d)\n", data, count)

	// 模拟文件读取操作
	tempFile := fmt.Sprintf("/tmp/task_%d_%d.txt", time.Now().Unix(), rand.Intn(1000))
	file, err := os.Create(tempFile)
	if err != nil {
		return "", fmt.Errorf("创建临时文件失败: %w", err)
	}
	defer os.Remove(tempFile)
	defer file.Close()

	// 模拟写入数据
	_, err = io.WriteString(file, fmt.Sprintf("Data: %s\nCount: %d\n", data, count))
	if err != nil {
		return "", fmt.Errorf("写入文件失败: %w", err)
	}

	// 模拟读取数据
	file.Seek(0, 0)
	content, err := io.ReadAll(file)
	if err != nil {
		return "", fmt.Errorf("读取文件失败: %w", err)
	}

	// 模拟处理延迟
	time.Sleep(150 * time.Millisecond)

	result := fmt.Sprintf("Processed: %s (count: %d, file_size: %d)", data, count, len(content))
	fmt.Printf("[ProcessData] 处理完成: %s\n", result)
	return result, nil
}

// DownloadFile 示例函数：下载文件（模拟长时间IO操作）
func DownloadFile(ctx context.Context, url string, timeout int) (string, error) {
	fmt.Printf("[DownloadFile] 开始下载: %s (timeout: %ds)\n", url, timeout)

	// 模拟下载延迟
	downloadTime := time.Duration(timeout) * time.Second
	if downloadTime > 2*time.Second {
		downloadTime = 2 * time.Second // 限制最大延迟
	}

	// 检查context是否已取消
	select {
	case <-ctx.Done():
		return "", fmt.Errorf("下载被取消: %w", ctx.Err())
	case <-time.After(downloadTime):
		// 继续执行
	}

	// 模拟下载进度
	for i := 0; i < 5; i++ {
		select {
		case <-ctx.Done():
			return "", fmt.Errorf("下载被取消: %w", ctx.Err())
		default:
			time.Sleep(downloadTime / 5)
			fmt.Printf("[DownloadFile] 下载进度: %d%%\n", (i+1)*20)
		}
	}

	result := fmt.Sprintf("Downloaded: %s (size: %d KB)", url, rand.Intn(1000)+100)
	fmt.Printf("[DownloadFile] 下载完成: %s\n", result)
	return result, nil
}

// ========== 自定义类型测试函数 ==========

// UserID 自定义类型：用户ID
type UserID string

// Order 自定义结构体：订单
type Order struct {
	ID     string `json:"id"`
	Amount int    `json:"amount"`
	Status string `json:"status"`
}

// ProcessOrder 使用自定义结构体的函数
func ProcessOrder(ctx context.Context, userID UserID, order Order) (string, error) {
	fmt.Printf("[ProcessOrder] 处理订单 - 用户ID: %s, 订单ID: %s, 金额: %d, 状态: %s\n",
		userID, order.ID, order.Amount, order.Status)

	// 模拟处理延迟
	time.Sleep(100 * time.Millisecond)

	result := fmt.Sprintf("订单 %s 处理完成，用户 %s，金额 %d", order.ID, userID, order.Amount)
	fmt.Printf("[ProcessOrder] %s\n", result)
	return result, nil
}

// ProcessUserList 使用slice类型的函数
func ProcessUserList(ctx context.Context, users []string) (int, error) {
	fmt.Printf("[ProcessUserList] 处理用户列表，共 %d 个用户\n", len(users))

	for i, user := range users {
		fmt.Printf("[ProcessUserList] 用户 %d: %s\n", i+1, user)
		time.Sleep(50 * time.Millisecond)
	}

	result := len(users)
	fmt.Printf("[ProcessUserList] 处理完成，共处理 %d 个用户\n", result)
	return result, nil
}

// ProcessConfig 使用map类型的函数
func ProcessConfig(ctx context.Context, config map[string]string) (string, error) {
	fmt.Printf("[ProcessConfig] 处理配置，共 %d 个配置项\n", len(config))

	for key, value := range config {
		fmt.Printf("[ProcessConfig] %s = %s\n", key, value)
	}

	time.Sleep(100 * time.Millisecond)
	result := fmt.Sprintf("配置处理完成，共 %d 项", len(config))
	fmt.Printf("[ProcessConfig] %s\n", result)
	return result, nil
}

// ProcessUserProfile 使用嵌套结构体的函数
type UserProfile struct {
	UserID   UserID            `json:"user_id"`
	Name     string            `json:"name"`
	Email    string            `json:"email"`
	Settings map[string]string `json:"settings"`
	Tags     []string          `json:"tags"`
}

func ProcessUserProfile(ctx context.Context, profile UserProfile) (string, error) {
	fmt.Printf("[ProcessUserProfile] 处理用户资料\n")
	fmt.Printf("  用户ID: %s\n", profile.UserID)
	fmt.Printf("  姓名: %s\n", profile.Name)
	fmt.Printf("  邮箱: %s\n", profile.Email)
	fmt.Printf("  设置项: %d\n", len(profile.Settings))
	fmt.Printf("  标签数: %d\n", len(profile.Tags))

	time.Sleep(150 * time.Millisecond)

	result := fmt.Sprintf("用户资料处理完成: %s (%s)", profile.Name, profile.UserID)
	fmt.Printf("[ProcessUserProfile] %s\n", result)
	return result, nil
}

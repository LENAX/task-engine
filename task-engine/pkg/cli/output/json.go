package output

import (
	"encoding/json"
	"os"

	"github.com/fatih/color"
)

// PrintJSON 输出JSON格式
func PrintJSON(data interface{}) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(data)
}

// PrintJSONString 输出JSON字符串
func PrintJSONString(data interface{}) (string, error) {
	bytes, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return "", err
	}
	return string(bytes), nil
}

// Success 输出成功消息
func Success(format string, args ...interface{}) {
	green := color.New(color.FgGreen, color.Bold)
	green.Printf("✅ "+format+"\n", args...)
}

// Error 输出错误消息
func Error(format string, args ...interface{}) {
	red := color.New(color.FgRed, color.Bold)
	red.Printf("❌ "+format+"\n", args...)
}

// Info 输出信息
func Info(format string, args ...interface{}) {
	cyan := color.New(color.FgCyan)
	cyan.Printf("ℹ️  "+format+"\n", args...)
}

// Warning 输出警告
func Warning(format string, args ...interface{}) {
	yellow := color.New(color.FgYellow)
	yellow.Printf("⚠️  "+format+"\n", args...)
}

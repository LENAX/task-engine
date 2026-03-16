package unit

import (
	"testing"

	"github.com/LENAX/task-engine/pkg/core/task"
)

// TestTaskStatus_IsSuccessStatus 验证成功状态判断对大小写不敏感
func TestTaskStatus_IsSuccessStatus(t *testing.T) {
	for _, s := range []string{"Success", "SUCCESS", "success"} {
		if !task.IsSuccessStatus(s) {
			t.Errorf("IsSuccessStatus(%q) 应为 true", s)
		}
	}
	if task.IsSuccessStatus("Failed") || task.IsSuccessStatus("") {
		t.Error("IsSuccessStatus 对非成功状态应为 false")
	}
}

// TestTaskStatus_IsFailedStatus 验证失败状态判断对大小写不敏感
func TestTaskStatus_IsFailedStatus(t *testing.T) {
	for _, s := range []string{"Failed", "FAILED", "failed"} {
		if !task.IsFailedStatus(s) {
			t.Errorf("IsFailedStatus(%q) 应为 true", s)
		}
	}
	if task.IsFailedStatus("Success") || task.IsFailedStatus("") {
		t.Error("IsFailedStatus 对非失败状态应为 false")
	}
}

// TestTaskStatus_IsTimeoutStatus 验证超时状态判断对大小写不敏感
func TestTaskStatus_IsTimeoutStatus(t *testing.T) {
	for _, s := range []string{"Timeout", "TIMEOUT", "timeout"} {
		if !task.IsTimeoutStatus(s) {
			t.Errorf("IsTimeoutStatus(%q) 应为 true", s)
		}
	}
	if task.IsTimeoutStatus("Success") || task.IsTimeoutStatus("") {
		t.Error("IsTimeoutStatus 对非超时状态应为 false")
	}
}

// TestTaskStatus_NormalizeTaskStatus 验证状态规范化
func TestTaskStatus_NormalizeTaskStatus(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"Success", task.TaskStatusSuccess},
		{"SUCCESS", task.TaskStatusSuccess},
		{"success", task.TaskStatusSuccess},
		{"Failed", task.TaskStatusFailed},
		{"FAILED", task.TaskStatusFailed},
		{"failed", task.TaskStatusFailed},
		{"Timeout", task.TaskStatusTimeout},
		{"TIMEOUT", task.TaskStatusTimeout},
		{"timeout", task.TaskStatusTimeout},
		{"unknown", "unknown"},
	}
	for _, tt := range tests {
		got := task.NormalizeTaskStatus(tt.in)
		if got != tt.want {
			t.Errorf("NormalizeTaskStatus(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// TestSubTaskResult_IsSuccess_IsFailed 验证 SubTaskResult 状态判断大小写不敏感
func TestSubTaskResult_IsSuccess_IsFailed(t *testing.T) {
	for _, status := range []string{"Success", "SUCCESS", "success"} {
		r := &task.SubTaskResult{Status: status}
		if !r.IsSuccess() {
			t.Errorf("SubTaskResult{Status: %q}.IsSuccess() 应为 true", status)
		}
		if r.IsFailed() {
			t.Errorf("SubTaskResult{Status: %q}.IsFailed() 应为 false", status)
		}
	}
	for _, status := range []string{"Failed", "FAILED", "failed"} {
		r := &task.SubTaskResult{Status: status}
		if r.IsSuccess() {
			t.Errorf("SubTaskResult{Status: %q}.IsSuccess() 应为 false", status)
		}
		if !r.IsFailed() {
			t.Errorf("SubTaskResult{Status: %q}.IsFailed() 应为 true", status)
		}
	}
}

package saga

// TransactionState 事务状态枚举（对外导出）
type TransactionState string

const (
	// TransactionStatePending 待处理状态（初始状态）
	TransactionStatePending TransactionState = "Pending"
	// TransactionStateCommitted 已提交状态（所有步骤成功）
	TransactionStateCommitted TransactionState = "Committed"
	// TransactionStateCompensating 补偿中状态（正在执行补偿）
	TransactionStateCompensating TransactionState = "Compensating"
	// TransactionStateCompensated 已补偿状态（补偿完成）
	TransactionStateCompensated TransactionState = "Compensated"
	// TransactionStateFailed 失败状态（补偿失败）
	TransactionStateFailed TransactionState = "Failed"
)

// IsValid 检查状态是否有效（对外导出）
func (s TransactionState) IsValid() bool {
	switch s {
	case TransactionStatePending,
		TransactionStateCommitted,
		TransactionStateCompensating,
		TransactionStateCompensated,
		TransactionStateFailed:
		return true
	default:
		return false
	}
}

// CanTransitionTo 检查是否可以转换到目标状态（对外导出）
func (s TransactionState) CanTransitionTo(target TransactionState) bool {
	switch s {
	case TransactionStatePending:
		// Pending可以转换到Committed或Compensating
		return target == TransactionStateCommitted || target == TransactionStateCompensating
	case TransactionStateCommitted:
		// Committed是终态，不能转换
		return false
	case TransactionStateCompensating:
		// Compensating可以转换到Compensated或Failed
		return target == TransactionStateCompensated || target == TransactionStateFailed
	case TransactionStateCompensated:
		// Compensated是终态，不能转换
		return false
	case TransactionStateFailed:
		// Failed是终态，不能转换
		return false
	default:
		return false
	}
}

// TransactionStep 事务步骤（对外导出）
// 记录已执行的步骤信息，用于补偿
type TransactionStep struct {
	TaskID      string      // 任务ID
	TaskName    string      // 任务名称
	Status      string      // 执行状态（Success/Failed）
	CompensateFuncName string // 补偿函数名称
	CompensateFuncID   string // 补偿函数ID
	ExecutedAt  int64       // 执行时间戳（Unix时间戳）
}

// NewTransactionStep 创建事务步骤（对外导出）
func NewTransactionStep(taskID, taskName, status, compensateFuncName, compensateFuncID string) *TransactionStep {
	return &TransactionStep{
		TaskID:            taskID,
		TaskName:          taskName,
		Status:            status,
		CompensateFuncName: compensateFuncName,
		CompensateFuncID:   compensateFuncID,
		ExecutedAt:        0, // 将在执行时设置
	}
}


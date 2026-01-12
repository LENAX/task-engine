package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/LENAX/task-engine/pkg/cli/output"
	"github.com/LENAX/task-engine/pkg/cli/taskengine"
)

var (
	instanceStatus string
	instanceLimit  int
)

// instanceCmd instance子命令
var instanceCmd = &cobra.Command{
	Use:   "instance",
	Short: "Instance管理命令",
	Long:  `管理Workflow执行实例，包括查看状态、暂停、恢复和取消。`,
}

// instanceListCmd 列出Instance
var instanceListCmd = &cobra.Command{
	Use:   "list",
	Short: "列出所有Instance",
	RunE: func(cmd *cobra.Command, args []string) error {
		client := taskengine.New(serverURL)
		result, err := client.ListInstances(instanceStatus, instanceLimit, 0)
		if err != nil {
			output.Error("查询失败: %v", err)
			return err
		}

		if outputJSON {
			return output.PrintJSON(result)
		}

		if len(result.Items) == 0 {
			output.Info("暂无Instance")
			return nil
		}

		table := output.NewTable([]string{"INSTANCE_ID", "WORKFLOW", "STATUS", "STARTED", "DURATION"})
		for _, inst := range result.Items {
			duration := "-"
			if inst.Duration != "" {
				duration = inst.Duration
			}
			table.AddRow([]string{
				inst.ID,
				inst.WorkflowName,
				formatStatus(inst.Status),
				inst.StartedAt.Format("2006-01-02 15:04:05"),
				duration,
			})
		}
		table.Render()
		return nil
	},
}

// instanceStatusCmd 查看Instance状态
var instanceStatusCmd = &cobra.Command{
	Use:   "status <id>",
	Short: "查看Instance执行状态",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client := taskengine.New(serverURL)

		// 获取Instance详情
		inst, err := client.GetInstance(args[0])
		if err != nil {
			output.Error("查询失败: %v", err)
			return err
		}

		// 获取Tasks
		tasks, err := client.GetInstanceTasks(args[0])
		if err != nil {
			output.Error("查询Tasks失败: %v", err)
			return err
		}

		if outputJSON {
			return output.PrintJSON(map[string]interface{}{
				"instance": inst,
				"tasks":    tasks,
			})
		}

		fmt.Printf("Instance: %s\n", inst.ID)
		fmt.Printf("Workflow: %s (%s)\n", inst.WorkflowName, inst.WorkflowID)
		fmt.Printf("Status:   %s\n", formatStatus(inst.Status))
		fmt.Printf("Progress: %d/%d (%d%%)\n",
			inst.Progress.Completed,
			inst.Progress.Total,
			calculatePercent(inst.Progress.Completed, inst.Progress.Total))
		fmt.Printf("Started:  %s\n", inst.StartedAt.Format("2006-01-02 15:04:05"))
		if inst.FinishedAt != nil {
			fmt.Printf("Finished: %s\n", inst.FinishedAt.Format("2006-01-02 15:04:05"))
		}
		if inst.ErrorMessage != "" {
			fmt.Printf("Error:    %s\n", inst.ErrorMessage)
		}

		fmt.Println("\nTasks:")
		for _, t := range tasks {
			statusIcon := getStatusIcon(t.Status)
			duration := ""
			if t.Duration != "" {
				duration = fmt.Sprintf(" %s", t.Duration)
			}
			fmt.Printf("  %s %s  %s%s\n", statusIcon, t.TaskName, t.Status, duration)
		}
		return nil
	},
}

// instanceHistoryCmd 查询执行历史
var instanceHistoryCmd = &cobra.Command{
	Use:   "history <workflow-id>",
	Short: "查询Workflow执行历史",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client := taskengine.New(serverURL)
		result, err := client.GetWorkflowHistory(args[0], instanceStatus, instanceLimit, 0)
		if err != nil {
			output.Error("查询失败: %v", err)
			return err
		}

		if outputJSON {
			return output.PrintJSON(result)
		}

		if len(result.Items) == 0 {
			output.Info("暂无执行历史")
			return nil
		}

		table := output.NewTable([]string{"INSTANCE_ID", "STATUS", "STARTED_AT", "DURATION", "ERROR"})
		for _, inst := range result.Items {
			duration := "-"
			if inst.Duration != "" {
				duration = inst.Duration
			}
			errMsg := "-"
			if inst.ErrorMessage != "" {
				if len(inst.ErrorMessage) > 30 {
					errMsg = inst.ErrorMessage[:30] + "..."
				} else {
					errMsg = inst.ErrorMessage
				}
			}
			table.AddRow([]string{
				inst.ID,
				formatStatus(inst.Status),
				inst.StartedAt.Format("2006-01-02 15:04:05"),
				duration,
				errMsg,
			})
		}
		table.Render()
		fmt.Printf("\n总计: %d 条记录\n", result.Total)
		return nil
	},
}

// instancePauseCmd 暂停Instance
var instancePauseCmd = &cobra.Command{
	Use:   "pause <id>",
	Short: "暂停Instance执行",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client := taskengine.New(serverURL)
		if err := client.PauseInstance(args[0]); err != nil {
			output.Error("暂停失败: %v", err)
			return err
		}
		output.Success("Instance已暂停: %s", args[0])
		return nil
	},
}

// instanceResumeCmd 恢复Instance
var instanceResumeCmd = &cobra.Command{
	Use:   "resume <id>",
	Short: "恢复Instance执行",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client := taskengine.New(serverURL)
		if err := client.ResumeInstance(args[0]); err != nil {
			output.Error("恢复失败: %v", err)
			return err
		}
		output.Success("Instance已恢复: %s", args[0])
		return nil
	},
}

// instanceCancelCmd 取消Instance
var instanceCancelCmd = &cobra.Command{
	Use:   "cancel <id>",
	Short: "取消Instance执行",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client := taskengine.New(serverURL)
		if err := client.CancelInstance(args[0]); err != nil {
			output.Error("取消失败: %v", err)
			return err
		}
		output.Success("Instance已取消: %s", args[0])
		return nil
	},
}

func init() {
	// 添加flags
	instanceListCmd.Flags().StringVar(&instanceStatus, "status", "", "按状态过滤 (Success/Failed/Running/Paused)")
	instanceListCmd.Flags().IntVar(&instanceLimit, "limit", 20, "返回记录数量限制")

	instanceHistoryCmd.Flags().StringVar(&instanceStatus, "status", "", "按状态过滤 (Success/Failed/Running/Paused)")
	instanceHistoryCmd.Flags().IntVar(&instanceLimit, "limit", 20, "返回记录数量限制")

	// 添加子命令
	instanceCmd.AddCommand(instanceListCmd)
	instanceCmd.AddCommand(instanceStatusCmd)
	instanceCmd.AddCommand(instanceHistoryCmd)
	instanceCmd.AddCommand(instancePauseCmd)
	instanceCmd.AddCommand(instanceResumeCmd)
	instanceCmd.AddCommand(instanceCancelCmd)
}

// formatStatus 格式化状态显示
func formatStatus(status string) string {
	switch status {
	case "Success":
		return "✅ Success"
	case "Failed":
		return "❌ Failed"
	case "Running":
		return "🔄 Running"
	case "Paused":
		return "⏸️  Paused"
	case "Pending", "Ready":
		return "⏳ Pending"
	case "Terminated":
		return "🛑 Terminated"
	default:
		return status
	}
}

// getStatusIcon 获取状态图标
func getStatusIcon(status string) string {
	switch status {
	case "Success":
		return "✅"
	case "Failed":
		return "❌"
	case "Running":
		return "🔄"
	case "Paused":
		return "⏸️"
	case "Pending", "Ready":
		return "⏳"
	case "Terminated":
		return "🛑"
	default:
		return "❓"
	}
}

// calculatePercent 计算百分比
func calculatePercent(completed, total int) int {
	if total == 0 {
		return 0
	}
	return completed * 100 / total
}

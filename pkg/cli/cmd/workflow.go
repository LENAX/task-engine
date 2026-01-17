package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/LENAX/task-engine/pkg/cli/output"
	"github.com/LENAX/task-engine/pkg/cli/taskengine"
)

// workflowCmd workflow子命令
var workflowCmd = &cobra.Command{
	Use:   "workflow",
	Short: "Workflow管理命令",
	Long:  `管理Workflow，包括上传、列出、查看、删除和执行。`,
}

// workflowListCmd 列出Workflow
var workflowListCmd = &cobra.Command{
	Use:   "list",
	Short: "列出所有Workflow",
	RunE: func(cmd *cobra.Command, args []string) error {
		client := taskengine.New(serverURL)
		result, err := client.ListWorkflows()
		if err != nil {
			output.Error("查询失败: %v", err)
			return err
		}

		if outputJSON {
			return output.PrintJSON(result)
		}

		if len(result.Items) == 0 {
			output.Info("暂无Workflow")
			return nil
		}

		table := output.NewTable([]string{"ID", "NAME", "TASKS", "CRON", "STATUS", "CREATED"})
		for _, wf := range result.Items {
			cronStr := "-"
			if wf.CronEnabled {
				cronStr = wf.CronExpr
			}
			table.AddRow([]string{
				wf.ID,
				wf.Name,
				fmt.Sprintf("%d", wf.TaskCount),
				cronStr,
				wf.Status,
				wf.CreatedAt.Format("2006-01-02 15:04:05"),
			})
		}
		table.Render()
		return nil
	},
}

// workflowShowCmd 查看Workflow详情
var workflowShowCmd = &cobra.Command{
	Use:   "show <id>",
	Short: "查看Workflow详情",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client := taskengine.New(serverURL)
		result, err := client.GetWorkflow(args[0])
		if err != nil {
			output.Error("查询失败: %v", err)
			return err
		}

		if outputJSON {
			return output.PrintJSON(result)
		}

		fmt.Printf("Workflow: %s\n", result.Name)
		fmt.Printf("ID:       %s\n", result.ID)
		fmt.Printf("描述:     %s\n", result.Description)
		fmt.Printf("状态:     %s\n", result.Status)
		if result.CronEnabled {
			fmt.Printf("定时:     %s\n", result.CronExpr)
		}
		fmt.Printf("任务数:   %d\n", result.TaskCount)
		fmt.Println("\nTasks:")
		for _, t := range result.Tasks {
			deps := ""
			if len(t.Dependencies) > 0 {
				deps = fmt.Sprintf(" (依赖: %v)", t.Dependencies)
			}
			fmt.Printf("  - %s%s\n", t.Name, deps)
		}
		return nil
	},
}

// workflowUploadCmd 上传Workflow
var workflowUploadCmd = &cobra.Command{
	Use:   "upload <file>",
	Short: "上传Workflow定义文件",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		content, err := os.ReadFile(args[0])
		if err != nil {
			output.Error("读取文件失败: %v", err)
			return err
		}

		client := taskengine.New(serverURL)
		result, err := client.UploadWorkflow(string(content))
		if err != nil {
			output.Error("上传失败: %v", err)
			return err
		}

		if outputJSON {
			return output.PrintJSON(result)
		}

		output.Success("Workflow上传成功: %s (%s)", result.Name, result.ID)
		return nil
	},
}

// workflowDeleteCmd 删除Workflow
var workflowDeleteCmd = &cobra.Command{
	Use:   "delete <id>",
	Short: "删除Workflow",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client := taskengine.New(serverURL)
		if err := client.DeleteWorkflow(args[0]); err != nil {
			output.Error("删除失败: %v", err)
			return err
		}

		output.Success("Workflow已删除: %s", args[0])
		return nil
	},
}

// workflowExecuteCmd 执行Workflow
var workflowExecuteCmd = &cobra.Command{
	Use:   "execute <id>",
	Short: "执行Workflow",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client := taskengine.New(serverURL)
		result, err := client.ExecuteWorkflow(args[0], nil)
		if err != nil {
			output.Error("执行失败: %v", err)
			return err
		}

		if outputJSON {
			return output.PrintJSON(result)
		}

		output.Success("Workflow已提交执行")
		fmt.Printf("Instance ID: %s\n", result.InstanceID)
		return nil
	},
}

func init() {
	workflowCmd.AddCommand(workflowListCmd)
	workflowCmd.AddCommand(workflowShowCmd)
	workflowCmd.AddCommand(workflowUploadCmd)
	workflowCmd.AddCommand(workflowDeleteCmd)
	workflowCmd.AddCommand(workflowExecuteCmd)
}

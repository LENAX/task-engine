package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

// 版本信息（编译时注入）
var (
	Version   = "1.0.9"
	GitCommit = "unknown"
	BuildTime = "unknown"
)

// versionCmd version命令
var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "显示版本信息",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("Task Engine CLI\n")
		fmt.Printf("  Version:    %s\n", Version)
		fmt.Printf("  Git Commit: %s\n", GitCommit)
		fmt.Printf("  Build Time: %s\n", BuildTime)
	},
}

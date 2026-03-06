package main

import (
	"fmt"
	"os"
	"strings"
)

func main() {
	if err := run(os.Args); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) < 2 {
		return usage()
	}

	configPath := GetConfigPath()
	cfg, err := LoadConfig(configPath)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	InitRunner(cfg.Machine.SSH)

	command := args[1]

	switch command {
	case "list":
		return listTasks(cfg)
	case "start":
		return handleStart(cfg, args[2:])
	case "stop":
		return handleStop(cfg, args[2:])
	case "restart":
		return handleRestart(cfg, args[2:])
	case "status":
		return handleStatus(cfg, args[2:])
	case "logs":
		return handleLogs(cfg, args[2:])
	default:
		return usage()
	}
}

func usage() error {
	fmt.Println("Usage: tmux-tasks <command> [task name]")
	fmt.Println()
	fmt.Println("Commands:")
	fmt.Println("  list                列出所有任务状态")
	fmt.Println("  start [name]        启动任务（无参数则启动所有）")
	fmt.Println("  stop [name]         停止任务（无参数则停止所有）")
	fmt.Println("  restart [name]      重启任务（无参数则重启所有）")
	fmt.Println("  status [name]       查看任务状态")
	fmt.Println("  logs [name]         查看任务日志")
	return nil
}

// 计算名称列的格式化宽度
func calcNameWidth(cfg *Config) int {
	minNameWidth := 4 // "名称" 视觉宽度
	for _, task := range cfg.Tasks {
		width := visualWidth(task.Name) + 2
		if width > minNameWidth {
			minNameWidth = width
		}
	}
	return minNameWidth
}

// 计算字符串的视觉宽度（中文算 2，英文算 1）
func visualWidth(s string) int {
	width := 0
	for _, r := range s {
		if r >= 0x4E00 && r <= 0x9FFF || // CJK Unified Ideographs
			r >= 0x3400 && r <= 0x4DBF || // CJK Unified Ideographs Extension A
			r >= 0xF900 && r <= 0xFAFF || // CJK Compatibility Ideographs
			r >= 0x3000 && r <= 0x303F || // CJK Symbols and Punctuation
			r >= 0xFF00 && r <= 0xFFEF { // Fullwidth Forms
			width += 2
		} else {
			width += 1
		}
	}
	return width
}

// 按视觉宽度填充字符串
func padVisual(s string, width int) string {
	w := visualWidth(s)
	if w >= width {
		return s
	}
	return s + strings.Repeat(" ", width-w)
}

func listTasks(cfg *Config) error {
	// 计算名称列最小宽度（基于最长名称 + 缓冲）
	minNameWidth := calcNameWidth(cfg)
	// 状态列宽度（RUNNING 是 7 字符）
	statusWidth := 10

	// 打印表头
	headerName := padVisual("名称", minNameWidth)
	headerStatus := padVisual("状态", statusWidth)
	fmt.Printf("\x1b[1m%s %s %s\x1b[0m\n", headerName, headerStatus, "描述")

	// 按配置顺序遍历
	for _, task := range cfg.Tasks {
		name := task.Name
		status, err := GetTaskStatus(name)
		if err != nil {
			continue
		}

		statusStr := "\x1b[31mSTOPPED\x1b[0m"
		if status.Running {
			statusStr = "\x1b[32mRUNNING\x1b[0m"
		}

		desc := task.Description
		if desc == "" {
			desc = "-"
		}

		fmt.Printf("%s %s %s\n", padVisual(name, minNameWidth), padVisual(statusStr, statusWidth), desc)
	}

	fmt.Println()
	return nil
}

func handleStart(cfg *Config, args []string) error {
	if len(args) == 0 {
		return startAllTasks(cfg)
	}
	// 单个任务时使用默认宽度
	return startOneTask(cfg, args[0], 0)
}

func handleStop(cfg *Config, args []string) error {
	if len(args) == 0 {
		return stopAllTasks(cfg)
	}
	return stopTask(cfg, args[0])
}

func handleRestart(cfg *Config, args []string) error {
	if len(args) == 0 {
		return restartAllTasks(cfg)
	}
	return restartTask(cfg, args[0])
}

func handleStatus(cfg *Config, args []string) error {
	if len(args) == 0 {
		return listTasks(cfg)
	}
	return statusTask(cfg, args[0])
}

func handleLogs(cfg *Config, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("请指定任务名称")
	}
	return logsTask(cfg, args[0])
}

func startAllTasks(cfg *Config) error {
	// 计算名称列宽度
	minNameWidth := calcNameWidth(cfg)

	// 打印表头
	headerName := padVisual("名称", minNameWidth)
	fmt.Printf("\x1b[1m%s %-10s %s\x1b[0m\n", headerName, "状态", "描述")

	// 按配置顺序遍历
	for _, task := range cfg.Tasks {
		startOneTask(cfg, task.Name, minNameWidth)
	}

	fmt.Println()
	return nil
}

func startOneTask(cfg *Config, name string, minNameWidth int) error {
	task, ok := cfg.GetTask(name)
	if !ok {
		return fmt.Errorf("任务 %s 不存在", name)
	}

	// 获取显示名称
	displayName := name
	if minNameWidth > 0 {
		displayName = padVisual(name, minNameWidth)
	}

	// 检查是否已存在
	if HasWindow(name) {
		// 检查是否正在运行
		status, err := GetTaskStatus(name)
		if err == nil && status.Running {
			fmt.Printf("%s %s %s\n", displayName, "\x1b[32mRUNNING\x1b[0m", "正在运行")
			return nil
		}
		// 存在但不运行，重启
		if err := RestartTask(task); err != nil {
			if strings.Contains(err.Error(), "already exists") {
				if err := StopTask(name); err != nil {
					fmt.Printf("%s %s %s\n", displayName, "\x1b[31mFAILED\x1b[0m", "停止失败")
					return err
				}
				if err := StartTask(task); err != nil {
					fmt.Printf("%s %s %s\n", displayName, "\x1b[31mFAILED\x1b[0m", "启动失败")
					return err
				}
			}
		}
		fmt.Printf("%s %s %s\n", displayName, "\x1b[33mRESTARTED\x1b[0m", "已重启")
		return nil
	}

	if err := StartTask(task); err != nil {
		fmt.Printf("%s %s %s\n", displayName, "\x1b[31mFAILED\x1b[0m", "启动失败")
		return err
	}
	fmt.Printf("%s %s %s\n", displayName, "\x1b[32mSTARTED\x1b[0m", "已启动")
	return nil
}

func stopAllTasks(cfg *Config) error {
	for _, task := range cfg.Tasks {
		if err := stopTask(cfg, task.Name); err != nil {
			fmt.Fprintf(os.Stderr, "停止 %s 失败: %v\n", task.Name, err)
		}
	}
	return nil
}

func stopTask(cfg *Config, name string) error {
	_, ok := cfg.GetTask(name)
	if !ok {
		return fmt.Errorf("任务 %s 不存在", name)
	}

	fmt.Printf("停止任务: %s\n", name)
	if err := StopTask(name); err != nil {
		return err
	}
	fmt.Printf("任务 %s 已停止\n", name)
	return nil
}

func restartAllTasks(cfg *Config) error {
	// 计算名称列宽度
	minNameWidth := calcNameWidth(cfg)

	// 打印表头
	headerName := padVisual("名称", minNameWidth)
	fmt.Printf("\x1b[1m%s %-10s %s\x1b[0m\n", headerName, "状态", "描述")

	for _, task := range cfg.Tasks {
		name := task.Name
		desc := task.Description
		if desc == "" {
			desc = "-"
		}

		if err := RestartTask(task); err != nil {
			fmt.Printf("%s \x1b[31mFAILED\x1b[0m %s\n", padVisual(name, minNameWidth), desc)
			fmt.Fprintf(os.Stderr, "重启 %s 失败: %v\n", name, err)
		} else {
			fmt.Printf("%s \x1b[32mOK\x1b[0m %s\n", padVisual(name, minNameWidth), desc)
		}
	}

	fmt.Println()
	return nil
}

func restartTask(cfg *Config, name string) error {
	task, ok := cfg.GetTask(name)
	if !ok {
		return fmt.Errorf("任务 %s 不存在", name)
	}

	fmt.Printf("重启任务: %s\n", name)
	if err := RestartTask(task); err != nil {
		return err
	}
	fmt.Printf("任务 %s 已重启\n", name)
	return nil
}

func statusTask(cfg *Config, name string) error {
	task, ok := cfg.GetTask(name)
	if !ok {
		return fmt.Errorf("任务 %s 不存在", name)
	}

	status, err := GetTaskStatus(name)
	if err != nil {
		return err
	}

	fmt.Printf("任务: %s\n", name)
	if task.Description != "" {
		fmt.Printf("描述: %s\n", task.Description)
	}
	if task.Command != "" {
		fmt.Printf("命令: %s\n", task.Command)
	}
	if task.Cwd != "" {
		fmt.Printf("工作目录: %s\n", task.Cwd)
	}

	running := "已停止"
	if status.Running {
		running = "运行中"
	}
	fmt.Printf("状态: %s\n", running)

	return nil
}

func logsTask(cfg *Config, name string) error {
	_, ok := cfg.GetTask(name)
	if !ok {
		return fmt.Errorf("任务 %s 不存在", name)
	}

	logs, err := GetTaskLogs(name, 100)
	if err != nil {
		return err
	}

	fmt.Print(strings.TrimSpace(logs))
	return nil
}

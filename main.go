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

func listTasks(cfg *Config) error {
	// 打印表头
	fmt.Printf("\x1b[1m%-15s %-10s %s\x1b[0m\n", "名称", "状态", "描述")

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

		fmt.Printf("%-15s %s %s\n", name, statusStr, desc)
	}

	fmt.Println()
	return nil
}

func handleStart(cfg *Config, args []string) error {
	if len(args) == 0 {
		return startAllTasks(cfg)
	}
	return startOneTask(cfg, args[0])
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
	// 打印表头
	fmt.Printf("\x1b[1m%-15s %-10s %s\x1b[0m\n", "名称", "状态", "描述")

	// 按配置顺序遍历
	for _, task := range cfg.Tasks {
		startOneTask(cfg, task.Name)
	}

	fmt.Println()
	return nil
}

func startOneTask(cfg *Config, name string) error {
	task, ok := cfg.GetTask(name)
	if !ok {
		return fmt.Errorf("任务 %s 不存在", name)
	}

	// 检查是否已存在
	if HasWindow(name) {
		// 检查是否正在运行
		status, err := GetTaskStatus(name)
		if err == nil && status.Running {
			fmt.Printf("%-15s %s %s\n", name, "\x1b[32mRUNNING\x1b[0m", "正在运行")
			return nil
		}
		// 存在但不运行，重启
		if err := RestartTask(task); err != nil {
			if strings.Contains(err.Error(), "already exists") {
				if err := StopTask(name); err != nil {
					fmt.Printf("%-15s %s %s\n", name, "\x1b[31mFAILED\x1b[0m", "停止失败")
					return err
				}
				if err := StartTask(task); err != nil {
					fmt.Printf("%-15s %s %s\n", name, "\x1b[31mFAILED\x1b[0m", "启动失败")
					return err
				}
			}
		}
		fmt.Printf("%-15s %s %s\n", name, "\x1b[33mRESTARTED\x1b[0m", "已重启")
		return nil
	}

	if err := StartTask(task); err != nil {
		fmt.Printf("%-15s %s %s\n", name, "\x1b[31mFAILED\x1b[0m", "启动失败")
		return err
	}
	fmt.Printf("%-15s %s %s\n", name, "\x1b[32mSTARTED\x1b[0m", "已启动")
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
	for _, task := range cfg.Tasks {
		if err := restartTask(cfg, task.Name); err != nil {
			fmt.Fprintf(os.Stderr, "重启 %s 失败: %v\n", task.Name, err)
		}
	}
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

	fmt.Println(logs)
	return nil
}

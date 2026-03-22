package main

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"github.com/mattn/go-runewidth"
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
	fmt.Println("  logs [-f] [name]    查看或跟踪任务日志（-f 实时跟随）")
	return nil
}

// 计算名称列的格式化宽度
func calcNameWidth(cfg *Config) int {
	minNameWidth := runewidth.StringWidth("名称")
	for _, task := range cfg.Tasks {
		width := runewidth.StringWidth(task.Name) + 2
		if width > minNameWidth {
			minNameWidth = width
		}
	}
	return minNameWidth
}

// 按视觉宽度填充字符串
func padVisual(s string, width int) string {
	return runewidth.FillRight(s, width)
}

func listTasks(cfg *Config) error {
	minNameWidth := calcNameWidth(cfg)
	statusWidth := 10

	headerName := padVisual("名称", minNameWidth)
	headerStatus := padVisual("状态", statusWidth)
	fmt.Printf("\x1b[1m%s %s %s\x1b[0m\n", headerName, headerStatus, "描述")

	// Group tasks by runner SSH destination
	runnerTasks := make(map[string][]Task)
	for _, task := range cfg.Tasks {
		r := NewRunnerForTask(cfg, task)
		runnerTasks[r.SSH] = append(runnerTasks[r.SSH], task)
	}

	var wg sync.WaitGroup
	var mu sync.Mutex
	statuses := make(map[string]*TaskStatus)

	for sshStr, tasks := range runnerTasks {
		wg.Add(1)
		go func(sshDest string, groupTasks []Task) {
			defer wg.Done()
			r := NewRunner(sshDest)
			sts, _ := r.GetAllTaskStatus()
			mu.Lock()
			for k, v := range sts {
				statuses[k] = v
			}
			mu.Unlock()
		}(sshStr, tasks)
	}
	wg.Wait()

	for _, task := range cfg.Tasks {
		name := task.Name
		statusStr := "\x1b[31mSTOPPED\x1b[0m"
		if st, ok := statuses[name]; ok && st.Running {
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

	follow := false
	var taskName string
	for _, arg := range args {
		if arg == "-f" || arg == "--follow" {
			follow = true
		} else {
			taskName = arg
		}
	}

	if taskName == "" {
		return fmt.Errorf("请指定任务名称")
	}

	return logsTask(cfg, taskName, follow)
}

func startAllTasks(cfg *Config) error {
	minNameWidth := calcNameWidth(cfg)
	headerName := padVisual("名称", minNameWidth)
	fmt.Printf("\x1b[1m%s %-10s %s\x1b[0m\n", headerName, "状态", "描述")

	var wg sync.WaitGroup
	results := make([]string, len(cfg.Tasks))

	for i, task := range cfg.Tasks {
		wg.Add(1)
		go func(idx int, t Task) {
			defer wg.Done()
			results[idx] = startOneTaskMsg(cfg, t.Name, minNameWidth)
		}(i, task)
	}
	wg.Wait()

	for _, res := range results {
		if res != "" {
			fmt.Print(res)
		}
	}
	fmt.Println()
	return nil
}

func startOneTask(cfg *Config, name string, minNameWidth int) error {
	msg := startOneTaskMsg(cfg, name, minNameWidth)
	fmt.Print(msg)
	return nil
}

func startOneTaskMsg(cfg *Config, name string, minNameWidth int) string {
	task, ok := cfg.GetTask(name)
	if !ok {
		return fmt.Sprintf("任务 %s 不存在\n", name)
	}

	runner := NewRunnerForTask(cfg, task)
	displayName := name
	if minNameWidth > 0 {
		displayName = padVisual(name, minNameWidth)
	}

	if runner.HasWindow(name) {
		status, err := runner.GetTaskStatus(name)
		if err == nil && status.Running {
			return fmt.Sprintf("%s %s %s\n", displayName, "\x1b[32mRUNNING\x1b[0m", "正在运行")
		}
		if err := runner.RestartTask(task); err != nil {
			if strings.Contains(err.Error(), "already exists") {
				if err := runner.StopTask(name); err != nil {
					WriteLog("Task %s failed to stop: %v", name, err)
					return fmt.Sprintf("%s %s %s\n", displayName, "\x1b[31mFAILED\x1b[0m", "停止失败")
				}
				if err := runner.StartTask(task); err != nil {
					WriteLog("Task %s failed to start after stop: %v", name, err)
					return fmt.Sprintf("%s %s %s\n", displayName, "\x1b[31mFAILED\x1b[0m", "启动失败")
				}
			}
		}
		return fmt.Sprintf("%s %s %s\n", displayName, "\x1b[33mRESTARTED\x1b[0m", "已重启")
	}

	if err := runner.StartTask(task); err != nil {
		WriteLog("Task %s failed to start: %v", name, err)
		return fmt.Sprintf("%s %s %s\n", displayName, "\x1b[31mFAILED\x1b[0m", "启动失败")
	}
	return fmt.Sprintf("%s %s %s\n", displayName, "\x1b[32mSTARTED\x1b[0m", "已启动")
}

func stopAllTasks(cfg *Config) error {
	var wg sync.WaitGroup
	for _, task := range cfg.Tasks {
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			if err := stopTask(cfg, name); err != nil {
				fmt.Fprintf(os.Stderr, "停止 %s 失败: %v\n", name, err)
			}
		}(task.Name)
	}
	wg.Wait()
	return nil
}

func stopTask(cfg *Config, name string) error {
	task, ok := cfg.GetTask(name)
	if !ok {
		return fmt.Errorf("任务 %s 不存在", name)
	}

	runner := NewRunnerForTask(cfg, task)
	fmt.Printf("停止任务: %s\n", name)
	if err := runner.StopTask(name); err != nil {
		return err
	}
	fmt.Printf("任务 %s 已停止\n", name)
	return nil
}

func restartAllTasks(cfg *Config) error {
	minNameWidth := calcNameWidth(cfg)
	headerName := padVisual("名称", minNameWidth)
	fmt.Printf("\x1b[1m%s %-10s %s\x1b[0m\n", headerName, "状态", "描述")

	var wg sync.WaitGroup
	results := make([]string, len(cfg.Tasks))

	for i, task := range cfg.Tasks {
		wg.Add(1)
		go func(idx int, t Task) {
			defer wg.Done()
			desc := t.Description
			if desc == "" {
				desc = "-"
			}
			r := NewRunnerForTask(cfg, t)
			if err := r.RestartTask(t); err != nil {
				WriteLog("Restart failed for %s: %v", t.Name, err)
				results[idx] = fmt.Sprintf("%s \x1b[31mFAILED\x1b[0m %s\n", padVisual(t.Name, minNameWidth), desc)
			} else {
				results[idx] = fmt.Sprintf("%s \x1b[32mOK\x1b[0m %s\n", padVisual(t.Name, minNameWidth), desc)
			}
		}(i, task)
	}
	wg.Wait()

	for _, res := range results {
		if res != "" {
			fmt.Print(res)
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

	runner := NewRunnerForTask(cfg, task)
	fmt.Printf("重启任务: %s\n", name)
	if err := runner.RestartTask(task); err != nil {
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

	runner := NewRunnerForTask(cfg, task)
	status, err := runner.GetTaskStatus(name)
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

func logsTask(cfg *Config, name string, follow bool) error {
	task, ok := cfg.GetTask(name)
	if !ok {
		return fmt.Errorf("任务 %s 不存在", name)
	}

	runner := NewRunnerForTask(cfg, task)
	if follow {
		return runner.FollowTaskLogs(name)
	}

	logs, err := runner.GetTaskLogs(name, 100)
	if err != nil {
		return err
	}

	lines := strings.Split(strings.ReplaceAll(logs, "\r\n", "\n"), "\n")
	for len(lines) > 0 {
		clean := strings.ReplaceAll(lines[len(lines)-1], "\x1b[0m", "")
		clean = strings.ReplaceAll(clean, "\x1b[K", "")
		clean = strings.ReplaceAll(clean, "\x1b[m", "")
		clean = strings.TrimSpace(clean)
		if clean == "" {
			lines = lines[:len(lines)-1]
		} else {
			break
		}
	}

	fmt.Println(strings.Join(lines, "\n"))
	return nil
}

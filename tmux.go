package main

import (
	"fmt"
	"os/exec"
	"strings"
)

const SessionName = "tmux-tasks"

type TaskStatus struct {
	Name     string
	Running  bool
	WindowID int
	PID      int
}

type Runner struct {
	SSH string
}

func NewRunner(ssh string) *Runner {
	if ssh == "" {
		return nil
	}
	return &Runner{SSH: ssh}
}

func (r *Runner) isRemote() bool {
	return r != nil && r.SSH != ""
}

func (r *Runner) runCmd(name string, args ...string) (string, error) {
	var cmd *exec.Cmd
	if r.isRemote() {
		remoteCmd := name + " " + strings.Join(args, " ")
		cmd = exec.Command("ssh", "-T", r.SSH, remoteCmd)
	} else {
		cmd = exec.Command(name, args...)
	}
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%s: %w", string(output), err)
	}
	return string(output), nil
}

func (r *Runner) HasSession() bool {
	_, err := r.runCmd("tmux", "has-session", "-t", SessionName)
	return err == nil
}

func (r *Runner) EnsureSession() error {
	if !r.HasSession() {
		_, err := r.runCmd("tmux", "new-session", "-d", "-s", SessionName, "sh")
		if err != nil {
			return err
		}
	}
	return nil
}

func (r *Runner) HasWindow(taskName string) bool {
	output, err := r.runCmd("tmux", "list-windows", "-t", SessionName, "-F", "'#{window_name}'")
	if err != nil {
		return false
	}
	return strings.Contains(output, taskName)
}

func (r *Runner) StartTask(task Task) error {
	if err := r.EnsureSession(); err != nil {
		return err
	}

	if r.HasWindow(task.Name) {
		return fmt.Errorf("window %s already exists", task.Name)
	}

	_, err := r.runCmd("tmux", "new-window", "-t", SessionName, "-n", task.Name)
	if err != nil {
		return err
	}

	if task.Command != "" {
		cmd := task.Command
		if task.Cwd != "" {
			cmd = "cd " + task.Cwd + " && " + cmd
		}
		// 使用 eval 展开路径并执行，确保 eval 和命令之间有空格
		evalCmd := "eval " + cmd
		_, err = r.runCmd("tmux", "send-keys", "-t", SessionName+":"+task.Name, "'"+evalCmd+"'", "Enter")
		if err != nil {
			return err
		}
	}

	return nil
}

func (r *Runner) StopTask(taskName string) error {
	if !r.HasWindow(taskName) {
		return fmt.Errorf("window %s does not exist", taskName)
	}

	_, err := r.runCmd("tmux", "kill-window", "-t", SessionName+":"+taskName)
	return err
}

func (r *Runner) RestartTask(task Task) error {
	if r.HasWindow(task.Name) {
		if err := r.StopTask(task.Name); err != nil {
			return err
		}
	}

	return r.StartTask(task)
}

func (r *Runner) GetTaskStatus(taskName string) (*TaskStatus, error) {
	status := &TaskStatus{
		Name:    taskName,
		Running: false,
	}

	// 检查窗口是否存在
	if !r.HasWindow(taskName) {
		return status, nil
	}

	// 窗口存在，标记为运行中
	status.Running = true

	// 获取 pane pid
	output, err := r.runCmd("tmux", "list-panes", "-t", SessionName+":"+taskName, "-F", "#{pane_pid}")
	if err == nil {
		pids := strings.TrimSpace(output)
		if pids != "" {
			fmt.Sscanf(pids, "%d", &status.PID)
		}
	}

	// 获取窗口索引
	output, err = r.runCmd("tmux", "list-windows", "-t", SessionName, "-F", "'#{window_index}:#{window_name}'")
	if err == nil {
		lines := strings.Split(output, "\n")
		for _, line := range lines {
			parts := strings.Split(line, ":")
			if len(parts) >= 2 && parts[1] == taskName {
				fmt.Sscanf(parts[0], "%d", &status.WindowID)
				break
			}
		}
	}

	return status, nil
}

func (r *Runner) GetTaskLogs(taskName string, lines int) (string, error) {
	if !r.HasWindow(taskName) {
		return "", fmt.Errorf("window %s does not exist", taskName)
	}

	return r.runCmd("tmux", "capture-pane", "-t", SessionName+":"+taskName, "-p", "-S", fmt.Sprintf("-%d", lines))
}

var defaultRunner *Runner

func InitRunner(ssh string) {
	defaultRunner = NewRunner(ssh)
}

func HasSession() bool {
	if defaultRunner == nil {
		return false
	}
	return defaultRunner.HasSession()
}

func HasWindow(taskName string) bool {
	if defaultRunner == nil {
		return false
	}
	return defaultRunner.HasWindow(taskName)
}

func StartTask(task Task) error {
	if defaultRunner == nil {
		return fmt.Errorf("not initialized")
	}
	return defaultRunner.StartTask(task)
}

func StopTask(taskName string) error {
	if defaultRunner == nil {
		return fmt.Errorf("not initialized")
	}
	return defaultRunner.StopTask(taskName)
}

func RestartTask(task Task) error {
	if defaultRunner == nil {
		return fmt.Errorf("not initialized")
	}
	return defaultRunner.RestartTask(task)
}

func GetTaskStatus(taskName string) (*TaskStatus, error) {
	if defaultRunner == nil {
		return nil, fmt.Errorf("not initialized")
	}
	return defaultRunner.GetTaskStatus(taskName)
}

func GetTaskLogs(taskName string, lines int) (string, error) {
	if defaultRunner == nil {
		return "", fmt.Errorf("not initialized")
	}
	return defaultRunner.GetTaskLogs(taskName, lines)
}

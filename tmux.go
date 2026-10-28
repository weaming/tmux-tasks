package main

import (
	"context"
	"fmt"
	"hash/fnv"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"time"
)

var SessionName = GetSessionName()

func GetSessionName() string {
	name := "tmux-tasks"
	nameFromEnv := os.Getenv("TMUX_SESSION_NAME")
	if nameFromEnv != "" {
		name = nameFromEnv
	}
	return name
}

type TaskStatus struct {
	Name     string
	Running  bool
	WindowID int
	PID      int
	Command  string
}

var shellCommands = map[string]struct{}{
	"bash": {}, "dash": {}, "fish": {}, "ksh": {}, "sh": {}, "tcsh": {}, "zsh": {},
}

func isShellCommand(command string) bool {
	command = strings.TrimPrefix(command, "-")
	if index := strings.LastIndex(command, "/"); index >= 0 {
		command = command[index+1:]
	}
	_, ok := shellCommands[command]
	return ok
}

type Runner struct {
	SSH         string
	Jump        JumpHosts
	InheritPath bool
}

func (r *Runner) isRemote() bool {
	return r != nil && r.SSH != ""
}

// Dest 返回 runner 目标的唯一标识，用于按目标分组
func (r *Runner) Dest() string {
	if len(r.Jump) == 0 {
		return r.SSH
	}
	return strings.Join(r.Jump, ",") + ">" + r.SSH
}

// controlDir 返回 SSH 连接复用 socket 目录，不可用时为空
var controlDir = func() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	dir := filepath.Join(home, ".ssh", "cm-tmux-tasks")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return ""
	}
	return dir
}()

// controlPath 生成连接复用 socket 路径，按中转链隔离避免复用错目标
func (r *Runner) controlPath() string {
	if controlDir == "" {
		return ""
	}
	if len(r.Jump) == 0 {
		return filepath.Join(controlDir, "%C")
	}
	h := fnv.New32a()
	h.Write([]byte(strings.Join(r.Jump, ",")))
	return filepath.Join(controlDir, fmt.Sprintf("j%08x-%%C", h.Sum32()))
}

// sshArgs 构造 ssh 公共选项；interactive 时不加 BatchMode 以允许人工输入密码
func (r *Runner) sshArgs(interactive bool) []string {
	args := []string{"-o", "ConnectTimeout=10"}
	if path := r.controlPath(); path != "" {
		args = append(args, "-o", "ControlMaster=auto", "-o", "ControlPath="+path, "-o", "ControlPersist=60s")
	}
	if !interactive {
		args = append(args, "-o", "BatchMode=yes")
	}
	if len(r.Jump) > 0 {
		// 经中转机时转发 agent，供后续跳数使用本地密钥
		args = append(args, "-A", "-J", strings.Join(r.Jump, ","))
	}
	return args
}

func (r *Runner) runCmd(name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var cmd *exec.Cmd
	if r.isRemote() {
		remoteCmd := name + " " + strings.Join(args, " ")
		sshArgs := append(r.sshArgs(false), "-T", r.SSH, remoteCmd)
		cmd = exec.CommandContext(ctx, "ssh", sshArgs...)
	} else {
		cmd = exec.CommandContext(ctx, name, args...)
	}
	WriteLog("Running: %s %s", name, strings.Join(args, " "))
	output, err := cmd.CombinedOutput()
	if err != nil {
		errStr := fmt.Sprintf("%s: %v", string(output), err)
		WriteLog("Error running command: %s", errStr)
		return "", fmt.Errorf("%s", errStr)
	}
	return string(output), nil
}

func (r *Runner) runScript(script string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var cmd *exec.Cmd
	if r.isRemote() {
		sshArgs := append(r.sshArgs(false), "-T", r.SSH, "sh", "-s")
		cmd = exec.CommandContext(ctx, "ssh", sshArgs...)
	} else {
		cmd = exec.CommandContext(ctx, "sh", "-s")
	}
	cmd.Stdin = strings.NewReader(script)
	WriteLog("Running Script:\n%s", script)
	output, err := cmd.CombinedOutput()
	if err != nil {
		errStr := fmt.Sprintf("%s: %v", string(output), err)
		WriteLog("Error running script: %s", errStr)
		return string(output), fmt.Errorf("%s", errStr)
	}
	return string(output), nil
}

func (r *Runner) HasWindow(taskName string) bool {
	script := fmt.Sprintf("tmux list-windows -t %s -F '#{window_name}' 2>/dev/null | grep -q '^%s$'", SessionName, taskName)
	_, err := r.runScript(script)
	return err == nil
}

func (r *Runner) StartTask(task Task) error {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("if ! tmux has-session -t %s 2>/dev/null; then\n", SessionName))
	sb.WriteString(fmt.Sprintf("  tmux new-session -d -s %s\n", SessionName))
	sb.WriteString(fmt.Sprintf("fi\n"))

	sb.WriteString(fmt.Sprintf("if tmux list-windows -t %s -F '#{window_name}' 2>/dev/null | grep -q '^%s$'; then\n", SessionName, task.Name))
	sb.WriteString(fmt.Sprintf("  echo 'window_exists'\n"))
	sb.WriteString(fmt.Sprintf("  exit 1\n"))
	sb.WriteString(fmt.Sprintf("fi\n"))

	sb.WriteString(fmt.Sprintf("tmux new-window -t %s -n %s\n", SessionName, task.Name))

	if task.Command != "" {
		cmd := task.Command
		taskEnvs := task.Env
		if taskEnvs == nil {
			taskEnvs = make(map[string]string)
		}
		if !r.isRemote() && r.InheritPath {
			if _, ok := taskEnvs["PATH"]; !ok {
				if hostPath := os.Getenv("PATH"); hostPath != "" {
					taskEnvs["PATH"] = hostPath + ":$PATH"
				}
			}
		}
		if len(taskEnvs) > 0 {
			var envs []string
			for k, v := range taskEnvs {
				envs = append(envs, k+"="+v)
			}
			cmd = "export " + strings.Join(envs, " ") + " && " + cmd
		}
		if task.Cwd != "" {
			cmd = "cd " + task.Cwd + " && " + cmd
		}
		escapedCmd := strings.ReplaceAll(cmd, "'", "'\\''")
		sb.WriteString(fmt.Sprintf("tmux send-keys -t %s:%s '%s' Enter\n", SessionName, task.Name, escapedCmd))
	}

	output, err := r.runScript(sb.String())
	if err != nil {
		if strings.Contains(output, "window_exists") {
			return fmt.Errorf("window %s already exists", task.Name)
		}
		return err
	}
	return nil
}

func (r *Runner) StopTask(taskName string) error {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("if tmux list-windows -t %s -F '#{window_name}' 2>/dev/null | grep -q '^%s$'; then\n", SessionName, taskName))
	sb.WriteString(fmt.Sprintf("  tmux kill-window -t %s:%s\n", SessionName, taskName))
	sb.WriteString(fmt.Sprintf("else\n"))
	sb.WriteString(fmt.Sprintf("  echo 'window_not_exist'\n"))
	sb.WriteString(fmt.Sprintf("  exit 1\n"))
	sb.WriteString(fmt.Sprintf("fi\n"))

	output, err := r.runScript(sb.String())
	if err != nil {
		if strings.Contains(output, "window_not_exist") {
			return fmt.Errorf("window %s does not exist", taskName)
		}
		return err
	}
	return nil
}

func (r *Runner) RestartTask(task Task) error {
	r.StopTask(task.Name)
	return r.StartTask(task)
}

func (r *Runner) GetAllTaskStatus() (map[string]*TaskStatus, error) {
	script := fmt.Sprintf("tmux list-panes -a -t %s -F '#{window_index}\t#{window_name}\t#{pane_pid}\t#{pane_current_command}\t#{pane_dead}' 2>/dev/null || true", SessionName)
	output, err := r.runScript(script)
	if err != nil {
		return nil, err
	}

	statuses := make(map[string]*TaskStatus)
	lines := strings.Split(strings.TrimSpace(output), "\n")
	for _, line := range lines {
		if line == "" {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) >= 5 {
			name := parts[1]
			command := parts[3]
			status := &TaskStatus{
				Name:    name,
				Command: command,
				Running: parts[4] != "1" && command != "" && !isShellCommand(command),
			}
			fmt.Sscanf(parts[0], "%d", &status.WindowID)
			fmt.Sscanf(parts[2], "%d", &status.PID)
			statuses[name] = status
		}
	}
	return statuses, nil
}

func (r *Runner) GetTaskStatus(taskName string) (*TaskStatus, error) {
	statuses, err := r.GetAllTaskStatus()
	if err != nil {
		return nil, err
	}
	if st, ok := statuses[taskName]; ok {
		return st, nil
	}
	return &TaskStatus{Name: taskName, Running: false}, nil
}

func (r *Runner) GetTaskLogs(taskName string, lines int) (string, error) {
	if !r.HasWindow(taskName) {
		return "", fmt.Errorf("window %s does not exist", taskName)
	}

	return r.runCmd("tmux", "capture-pane", "-e", "-t", SessionName+":"+taskName, "-p", "-S", fmt.Sprintf("-%d", lines))
}

func (r *Runner) FollowTaskLogs(taskName string) error {
	var cmd *exec.Cmd
	if r.isRemote() {
		sshArgs := append(r.sshArgs(true), "-t", r.SSH, "tmux", "attach-session", "-t", SessionName+":"+taskName, "-r")
		cmd = exec.Command("ssh", sshArgs...)
	} else {
		cmd = exec.Command("tmux", "attach-session", "-t", SessionName+":"+taskName, "-r")
	}
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return err
	}

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	go func() {
		<-c
		if cmd.Process != nil {
			cmd.Process.Kill()
		}
		os.Exit(0)
	}()

	return cmd.Wait()
}

func NewRunnerForTask(cfg *Config, task Task) *Runner {
	ssh := ""
	var jump JumpHosts
	if cfg.Machine != nil {
		ssh = cfg.Machine.SSH
		jump = cfg.Machine.Jump
	}
	if task.SSH != nil {
		ssh = *task.SSH
	}
	// 任务显式配置 jump 时覆盖 machine 级（含 jump: [] 表示直连）
	if task.Jump != nil {
		jump = task.Jump
	}
	if ssh == "local" || ssh == "" {
		ssh = ""
		jump = nil
	}
	inheritPath := cfg.InheritPath || isInDocker()
	return &Runner{SSH: ssh, Jump: jump, InheritPath: inheritPath}
}

func isInDocker() bool {
	_, err := os.Stat("/.dockerenv")
	return err == nil
}

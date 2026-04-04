package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
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
}

type Runner struct {
	SSH         string
	InheritPath bool
}

func NewRunner(ssh string) *Runner {
	return &Runner{SSH: ssh}
}

func (r *Runner) isRemote() bool {
	return r != nil && r.SSH != ""
}

func (r *Runner) runCmd(name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var cmd *exec.Cmd
	if r.isRemote() {
		remoteCmd := name + " " + strings.Join(args, " ")
		cmd = exec.CommandContext(ctx, "ssh", "-T", r.SSH, remoteCmd)
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
		cmd = exec.CommandContext(ctx, "ssh", "-T", r.SSH, "sh", "-s")
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

func (r *Runner) HasSession() bool {
	_, err := r.runCmd("tmux", "has-session", "-t", SessionName)
	return err == nil
}

func (r *Runner) EnsureSession() error {
	if !r.HasSession() {
		_, err := r.runCmd("tmux", "new-session", "-d", "-s", SessionName)
		if err != nil {
			return err
		}
	}
	return nil
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
	script := fmt.Sprintf("tmux list-panes -a -F '#{session_name}:#{window_index}:#{window_name}:#{pane_pid}' 2>/dev/null | grep '^%s:' || true", SessionName)
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
		parts := strings.Split(line, ":")
		if len(parts) >= 4 {
			// parts[0] = session_name
			// parts[1] = window_index
			// parts[2] = window_name
			// parts[3] = pane_pid
			name := parts[2]
			status := &TaskStatus{
				Name:    name,
				Running: true,
			}
			fmt.Sscanf(parts[1], "%d", &status.WindowID)
			fmt.Sscanf(parts[3], "%d", &status.PID)
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
		cmd = exec.Command("ssh", "-t", r.SSH, "tmux", "attach-session", "-t", SessionName+":"+taskName, "-r")
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
	if cfg.Machine != nil {
		ssh = cfg.Machine.SSH
	}
	if task.SSH != nil {
		ssh = *task.SSH
	}
	if ssh == "local" || ssh == "" {
		ssh = ""
	}
	inheritPath := cfg.InheritPath || isInDocker()
	return &Runner{SSH: ssh, InheritPath: inheritPath}
}

func isInDocker() bool {
	_, err := os.Stat("/.dockerenv")
	return err == nil
}

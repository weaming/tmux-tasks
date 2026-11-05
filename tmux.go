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

// sshArgs 构造 ssh 公共选项；interactive 时不加 BatchMode 以允许人工输入密码
// 连接复用交给 ~/.ssh/config（ControlMaster/ControlPath），这样能复用用户已有 master
func (r *Runner) sshArgs(interactive bool) []string {
	args := []string{"-o", "ConnectTimeout=10"}
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

func (r *Runner) ListWindows() ([]string, error) {
	script := fmt.Sprintf(`if tmux has-session -t %[1]s 2>/dev/null; then
  tmux list-windows -t %[1]s -F '#{window_name}'
fi`, SessionName)
	output, err := r.runScript(script)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(output) == "" {
		return nil, nil
	}
	return strings.Split(strings.TrimSpace(output), "\n"), nil
}

// windowRef 返回 tmux 的 session:window 目标
func windowRef(taskName string) string {
	return SessionName + ":" + taskName
}

// shellQuote 把命令包成单引号字面量，供送进 pane 的行使用
func shellQuote(cmd string) string {
	return "'" + strings.ReplaceAll(cmd, "'", `'\''`) + "'"
}

// taskCommand 组装送进 pane 的完整命令行：env、cwd、可选继承宿主 PATH
func (r *Runner) taskCommand(task Task) string {
	if task.Command == "" {
		return ""
	}

	cmd := task.Command
	envs := task.Env
	if envs == nil {
		envs = make(map[string]string)
	}
	if !r.isRemote() && r.InheritPath {
		if _, ok := envs["PATH"]; !ok {
			if hostPath := os.Getenv("PATH"); hostPath != "" {
				envs["PATH"] = hostPath + ":$PATH"
			}
		}
	}
	if len(envs) > 0 {
		pairs := make([]string, 0, len(envs))
		for k, v := range envs {
			pairs = append(pairs, k+"="+v)
		}
		cmd = "export " + strings.Join(pairs, " ") + " && " + cmd
	}
	if task.Cwd != "" {
		cmd = "cd " + task.Cwd + " && " + cmd
	}
	return cmd
}

func (r *Runner) StartTask(task Task) error {
	sendKeys := ""
	if cmd := r.taskCommand(task); cmd != "" {
		sendKeys = fmt.Sprintf("tmux send-keys -t %s %s Enter\n", windowRef(task.Name), shellQuote(cmd))
	}

	script := fmt.Sprintf(`created=0
if ! tmux has-session -t %[1]s 2>/dev/null; then
  tmux new-session -d -s %[1]s -n %[2]s && created=1
fi
if [ "$created" != 1 ]; then
  if tmux list-windows -t %[1]s -F '#{window_name}' 2>/dev/null | grep -q '^%[2]s$'; then
    echo 'window_exists'
    exit 1
  fi
  tmux new-window -t %[1]s -n %[2]s || exit 1
fi
%[3]s`, SessionName, task.Name, sendKeys)

	output, err := r.runScript(script)
	if err != nil {
		if strings.Contains(output, "window_exists") {
			return fmt.Errorf("window %s already exists", task.Name)
		}
		return err
	}
	return nil
}

// StopTask 结束窗口，并补发 SIGTERM/SIGKILL 清掉窗口内的残留进程
// kill-window 只会让 pane 内的进程收到 SIGHUP，把 SIGHUP 当"重载配置"的程序会变成孤儿继续占用端口
func (r *Runner) StopTask(taskName string) error {
	script := fmt.Sprintf(`if ! tmux list-windows -t %[1]s -F '#{window_name}' 2>/dev/null | grep -q '^%[2]s$'; then
  echo 'window_not_exist'
  exit 1
fi

# 先按进程树记下窗口内的所有进程，kill-window 后它们会脱离父进程，届时无法再查找
pane_pid=$(tmux list-panes -t %[3]s -F '#{pane_pid}' | head -n 1)
pids=''
if [ -n "$pane_pid" ]; then
  pids=$(ps -eo pid=,ppid= | awk -v root="$pane_pid" '
    { parent[$1] = $2 }
    END {
      keep[root] = 1
      do {
        added = 0
        for (p in parent) {
          if (!(p in keep) && (parent[p] in keep)) { keep[p] = 1; added = 1 }
        }
      } while (added)
      for (p in keep) printf "%%s ", p
    }')
fi

tmux kill-window -t %[3]s

# 先 TERM 等其退出，超时再 KILL，避免新旧进程抢同一端口
if [ -n "$pids" ]; then
  kill -TERM $pids 2>/dev/null
  i=0
  while [ $i -lt 30 ]; do
    alive=''
    for p in $pids; do
      st=$(ps -o stat= -p "$p" 2>/dev/null)
      case "$st" in ''|Z*) ;; *) alive="$alive $p" ;; esac
    done
    [ -z "$alive" ] && break
    sleep 0.1
    i=$((i+1))
  done
  if [ -n "$alive" ]; then kill -KILL $alive 2>/dev/null; fi
fi
`, SessionName, taskName, windowRef(taskName))

	output, err := r.runScript(script)
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

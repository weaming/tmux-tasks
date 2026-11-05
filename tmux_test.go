package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestIsShellCommand(t *testing.T) {
	tests := []struct {
		command string
		want    bool
	}{
		{command: "zsh", want: true},
		{command: "-bash", want: true},
		{command: "/bin/fish", want: true},
		{command: "remote-shell-mcp", want: false},
		{command: "node", want: false},
	}

	for _, test := range tests {
		if got := isShellCommand(test.command); got != test.want {
			t.Errorf("isShellCommand(%q) = %t, want %t", test.command, got, test.want)
		}
	}
}

func writeConfig(t *testing.T, content string) *Config {
	t.Helper()
	path := filepath.Join(t.TempDir(), "tasks.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	return cfg
}

// 准备隔离的 tmux 测试环境：临时 TMUX_TMPDIR + 独立 session 名，cleanup 时清理
func setupTestTmuxSession(t *testing.T) {
	t.Helper()

	tmuxDir, err := os.MkdirTemp("/tmp", "tt-")
	if err != nil {
		t.Fatalf("创建 tmux 临时目录: %v", err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(tmuxDir); err != nil {
			t.Errorf("删除 tmux 临时目录: %v", err)
		}
	})
	t.Setenv("TMUX_TMPDIR", tmuxDir)
	t.Setenv("TMUX", "")

	previousSession := SessionName
	SessionName = "tmux-tasks-test-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	t.Cleanup(func() {
		if exec.Command("tmux", "has-session", "-t", SessionName).Run() == nil {
			if output, err := exec.Command("tmux", "kill-session", "-t", SessionName).CombinedOutput(); err != nil {
				t.Errorf("结束测试 session: %v: %s", err, output)
			}
		}
		SessionName = previousSession
	})
}

func TestJumpInheritAndOverride(t *testing.T) {
	cfg := writeConfig(t, `
machine:
  ssh: prod
  jump: hk

tasks:
  inherit:
    command: echo inherit
  multi:
    ssh: other
    jump: [hk, hk2]
    command: echo multi
  direct:
    ssh: direct-host
    jump: []
    command: echo direct
  localTask:
    ssh: local
    command: echo local
`)

	tests := []struct {
		task     string
		wantSSH  string
		wantJump []string
	}{
		{task: "inherit", wantSSH: "prod", wantJump: []string{"hk"}},
		{task: "multi", wantSSH: "other", wantJump: []string{"hk", "hk2"}},
		{task: "direct", wantSSH: "direct-host", wantJump: nil},
		{task: "localTask", wantSSH: "", wantJump: nil},
	}

	for _, test := range tests {
		task, ok := cfg.GetTask(test.task)
		if !ok {
			t.Fatalf("task %s not found", test.task)
		}
		runner := NewRunnerForTask(cfg, task)
		if runner.SSH != test.wantSSH {
			t.Errorf("task %s ssh = %q, want %q", test.task, runner.SSH, test.wantSSH)
		}
		if !slices.Equal(runner.Jump, test.wantJump) {
			t.Errorf("task %s jump = %v, want %v", test.task, runner.Jump, test.wantJump)
		}
	}
}

func TestJumpHostsEmptyListIsNil(t *testing.T) {
	cfg := writeConfig(t, `
tasks:
  plain:
    ssh: host
    command: echo plain
`)

	task, _ := cfg.GetTask("plain")
	if task.Jump != nil {
		t.Errorf("未配置 jump 时不应被当成显式清空, got %v", task.Jump)
	}
}

func TestDescAlias(t *testing.T) {
	cfg := writeConfig(t, `
tasks:
  short:
    command: echo short
    desc: 简写描述
  long:
    command: echo long
    description: 完整描述
  both:
    command: echo both
    description: 完整优先
    desc: 不应生效
`)

	tests := []struct {
		task string
		want string
	}{
		{task: "short", want: "简写描述"},
		{task: "long", want: "完整描述"},
		{task: "both", want: "完整优先"},
	}

	for _, test := range tests {
		task, ok := cfg.GetTask(test.task)
		if !ok {
			t.Fatalf("task %s not found", test.task)
		}
		if task.Description != test.want {
			t.Errorf("task %s description = %q, want %q", test.task, task.Description, test.want)
		}
	}
}

func TestSSHArgsWithJump(t *testing.T) {
	runner := &Runner{SSH: "prod", Jump: JumpHosts{"hk", "hk2"}}

	args := runner.sshArgs(false)
	if !containsPair(args, "-J", "hk,hk2") {
		t.Errorf("sshArgs 缺少 -J hk,hk2: %v", args)
	}
	if !slices.Contains(args, "-A") {
		t.Errorf("经中转时应转发 agent: %v", args)
	}
	if !containsPair(args, "-o", "BatchMode=yes") {
		t.Errorf("非交互模式应带 BatchMode: %v", args)
	}
	if containsPair(args, "-o", "ControlMaster=auto") || slices.ContainsFunc(args, func(arg string) bool {
		return strings.HasPrefix(arg, "ControlPath=")
	}) {
		t.Errorf("应跟随 ~/.ssh/config，不覆盖连接复用: %v", args)
	}

	interactive := runner.sshArgs(true)
	if containsPair(interactive, "-o", "BatchMode=yes") {
		t.Errorf("交互模式不应带 BatchMode: %v", interactive)
	}

	local := &Runner{SSH: "prod"}
	if slices.Contains(local.sshArgs(false), "-A") {
		t.Errorf("直连不应转发 agent: %v", local.sshArgs(false))
	}
}

func TestDestIsolatesJumpChain(t *testing.T) {
	direct := &Runner{SSH: "prod"}
	viaHK := &Runner{SSH: "prod", Jump: JumpHosts{"hk"}}
	viaJP := &Runner{SSH: "prod", Jump: JumpHosts{"jp"}}

	if direct.Dest() == viaHK.Dest() {
		t.Errorf("直连与中转目标应区分: %q", direct.Dest())
	}
	if viaHK.Dest() == viaJP.Dest() {
		t.Errorf("不同中转链应区分: %q", viaHK.Dest())
	}
}

func TestSummarizeErr(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "多行 ssh 错误取首个有效行",
			err:  errors.New("Warning: Permanently added 'jp' to the list\n\nConnection timed out during banner exchange\nConnection to UNKNOWN port 65535 timed out: exit status 255"),
			want: "Connection timed out during banner exchange",
		},
		{
			name: "空错误",
			err:  errors.New("  \n"),
			want: "未知错误",
		},
	}

	for _, test := range tests {
		if got := summarizeErr(test.err); got != test.want {
			t.Errorf("%s: summarizeErr() = %q, want %q", test.name, got, test.want)
		}
	}
}

func containsPair(args []string, key, value string) bool {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == key && args[i+1] == value {
			return true
		}
	}
	return false
}

func TestStopMissingTasksBeforeStartRestartAutoStart(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux 未安装")
	}

	for _, command := range []string{"start", "restart", "autostart"} {
		t.Run(command, func(t *testing.T) {
			setupTestTmuxSession(t)

			cfg := writeConfig(t, `
tasks:
  keep:
    command: sleep 60
    autoStart: true
`)
			if output, err := exec.Command("tmux", "new-session", "-d", "-s", SessionName, "-n", "keep").CombinedOutput(); err != nil {
				t.Fatalf("创建测试 session: %v: %s", err, output)
			}
			if output, err := exec.Command("tmux", "new-window", "-t", SessionName, "-n", "orphan").CombinedOutput(); err != nil {
				t.Fatalf("创建遗留窗口: %v: %s", err, output)
			}

			var err error
			switch command {
			case "start":
				err = handleStart(cfg, []string{"keep"})
			case "restart":
				err = handleRestart(cfg, []string{"keep"})
			case "autostart":
				err = handleAutoStart(cfg)
			}
			if err != nil {
				t.Fatalf("%s 失败: %v", command, err)
			}

			windows, err := (&Runner{}).ListWindows()
			if err != nil {
				t.Fatalf("查询窗口: %v", err)
			}
			if !slices.Equal(windows, []string{"keep"}) {
				t.Errorf("窗口 = %v，期望仅保留 keep", windows)
			}
		})
	}
}

func TestAutoStartStopsWindowsWhenConfigHasNoTasks(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux 未安装")
	}

	setupTestTmuxSession(t)

	if output, err := exec.Command("tmux", "new-session", "-d", "-s", SessionName, "-n", "orphan").CombinedOutput(); err != nil {
		t.Fatalf("创建遗留窗口: %v: %s", err, output)
	}
	if err := handleAutoStart(writeConfig(t, "tasks: {}\n")); err != nil {
		t.Fatalf("autostart 失败: %v", err)
	}
	windows, err := (&Runner{}).ListWindows()
	if err != nil {
		t.Fatalf("查询窗口: %v", err)
	}
	if len(windows) != 0 {
		t.Errorf("仍有遗留窗口: %v", windows)
	}
}

// 按父进程查找指定命令名的子进程 pid
func findChildPid(parentPid int, comm string) int {
	output, err := exec.Command("ps", "-eo", "pid=,ppid=,comm=").Output()
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(output), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		ppid, err := strconv.Atoi(fields[1])
		if err != nil || ppid != parentPid {
			continue
		}
		if filepath.Base(fields[2]) != comm {
			continue
		}
		if pid, err := strconv.Atoi(fields[0]); err == nil {
			return pid
		}
	}
	return 0
}

func isProcessAlive(pid int) bool {
	output, err := exec.Command("ps", "-o", "stat=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return false
	}
	stat := strings.TrimSpace(string(output))
	return stat != "" && !strings.HasPrefix(stat, "Z")
}

// 停任务时 kill-window 只发 SIGHUP，忽略 SIGHUP 的程序必须被补刀，否则会漏成孤儿占端口
func TestStopTaskKillsChildIgnoringHup(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux 未安装")
	}
	setupTestTmuxSession(t)

	runner := &Runner{}
	// 子进程继承被忽略的 SIGHUP，模拟把 SIGHUP 当"重载配置"的守护进程
	task := Task{Name: "stubborn", Command: `sh -c 'trap "" HUP; exec sleep 60'`}
	if err := runner.StartTask(task); err != nil {
		t.Fatalf("启动任务: %v", err)
	}

	var sleepPid int
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		output, err := exec.Command("tmux", "list-panes", "-t", SessionName+":stubborn", "-F", "#{pane_pid}").Output()
		if err == nil {
			if panePid, err := strconv.Atoi(strings.TrimSpace(string(output))); err == nil {
				if pid := findChildPid(panePid, "sleep"); pid > 0 {
					sleepPid = pid
					break
				}
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	if sleepPid == 0 {
		t.Fatal("任务进程未启动")
	}

	if err := runner.StopTask("stubborn"); err != nil {
		t.Fatalf("停止任务: %v", err)
	}

	if isProcessAlive(sleepPid) {
		_ = exec.Command("kill", "-9", strconv.Itoa(sleepPid)).Run()
		t.Errorf("停止任务后进程 %d 仍存活", sleepPid)
	}
}

package main

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
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
	if containsPair(args, "-o", "ControlMaster=auto") != (controlDir != "") {
		t.Errorf("连接复用选项与 controlDir 状态不一致: %v, controlDir=%q", args, controlDir)
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
	if viaHK.controlPath() != viaHK.controlPath() {
		t.Error("controlPath 应稳定")
	}
	if viaHK.controlPath() == viaJP.controlPath() {
		t.Errorf("不同中转链应使用不同复用 socket: %q", viaHK.controlPath())
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

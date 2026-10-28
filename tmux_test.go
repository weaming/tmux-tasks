package main

import "testing"

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

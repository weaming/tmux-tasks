package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

func WriteLog(format string, a ...interface{}) {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	logFile := filepath.Join(home, ".tmux-tasks.log")
	f, err := os.OpenFile(logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()

	// 格式化时间，香港+8时区 ISO 格式
	loc := time.FixedZone("HKT", 8*3600)
	now := time.Now().In(loc).Format(time.RFC3339)
	msg := fmt.Sprintf(format, a...)

	fmt.Fprintf(f, "[%s] %s\n", now, msg)
}

#!/usr/bin/env bash

# 装到 PATH 中优先级最高的 GOPATH/bin，避免 build 后被 ~/.bin 下的旧副本遮住
target="$(go env GOPATH)/bin/tmux-tasks"
go build -ldflags "-s -w" -trimpath -o "$target" .
echo "$target"

#!/usr/bin/env bash

go build -ldflags "-s -w"  -trimpath -o ~/.bin/tmux-tasks .
echo '~/.bin/tmux-tasks'

#!/usr/bin/env fish
# 浮窗内模糊搜索并切换窗口（跨会话）

if not command -q fzf
    echo "未找到 fzf，请先安装：brew install fzf"
    sleep 2
    exit 1
end

set -l items (tmux list-windows -a -F '#{session_name}:#{window_index} #{window_name}#{?window_active, ●,}')
set -l target (string join \n $items | fzf --reverse --prompt='窗口 > ' --header='Enter 切换 / Esc 取消')
or exit 0

set -l win (string split -m1 ' ' $target)[1]
set -l parts (string split -m1 ':' $win)
set -l target_win "$parts[1]:$parts[2]"

tmux switch-client -t $target_win
or tmux select-window -t $target_win

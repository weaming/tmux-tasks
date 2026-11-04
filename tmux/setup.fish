#!/usr/bin/env fish
# 将 tmux 配置软链到用户目录

set -l CONFIG_DIR ~/.config/tmux
set -l SRC_DIR (path resolve (dirname (status filename)))
mkdir -p $CONFIG_DIR ~/.cache/tmux

ln -sfn $SRC_DIR/tmux.conf $CONFIG_DIR/tmux.conf
and echo "✓ $CONFIG_DIR/tmux.conf"

ln -sfn $SRC_DIR/popup-find.fish $CONFIG_DIR/popup-find.fish
and echo "✓ $CONFIG_DIR/popup-find.fish"

if test -e ~/.tmux.conf
    echo "⚠ 已存在 ~/.tmux.conf，tmux 会优先读取它，请确认无冲突"
end

tmux has-session 2>/dev/null
and tmux source-file $CONFIG_DIR/tmux.conf
and echo "✓ 已重载运行中的 tmux"

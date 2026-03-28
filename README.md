# tmux-tasks

命令行管理 tmux 后台任务

## 安装

```bash
bash build.sh
```

## 配置

创建 `tasks.yaml`：

```yaml
machine:
  ssh: hk

tasks:
  webhook:
    command: ~/bin/webhook -c ~/.dotfiles/env/weaming-hooks.yaml
    description: webhook 服务

  v2ray-profiles:
    command: markdir -no-index '/'
    cwd: ~/my-docs
    description: 文档管理
```

## 使用

```bash
tmux-tasks list             # 列出任务
tmux-tasks start            # 启动所有任务
tmux-tasks start <name>     # 启动指定任务
tmux-tasks stop             # 停止所有任务
tmux-tasks stop <name>      # 停止指定任务
tmux-tasks restart          # 重启所有任务
tmux-tasks logs <name>      # 查看日志
```

## 环境变量

- `TMUX_TASKS` - 配置文件路径

## 任务配置

| 字段        | 说明       |
| ----------- | ---------- |
| command     | 执行的命令 |
| cwd         | 工作目录   |
| env         | 环境变量   |
| description | 描述       |

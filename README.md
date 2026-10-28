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
  jump: bastion      # 可选，经中转机访问 machine.ssh

tasks:
  webhook:
    command: ~/bin/webhook -c ~/.dotfiles/env/weaming-hooks.yaml
    description: webhook 服务

  v2ray-profiles:
    command: markdir -no-index '/'
    cwd: ~/my-docs
    description: 文档管理

  inner-api:
    ssh: 10.0.0.8    # 内网机器，经 bastion 中转
    command: ./api
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

| 字段        | 说明                                                     |
| ----------- | -------------------------------------------------------- |
| command     | 执行的命令                                               |
| cwd         | 工作目录                                                 |
| env         | 环境变量                                                 |
| ssh         | 目标主机，`local` 表示本机；未设置时用 `machine.ssh`     |
| jump        | 中转机，`hk` 或 `[hk, hk2]`，生成 `ssh -J`；`[]` 表示直连 |
| autoStart   | `autostart` 命令是否启动该任务                           |
| disabled    | 排除在全部/通配符操作之外                                |
| description | 描述                                                     |

## SSH 中转

经中转机（跳板机）访问内网机器，`jump` 会转换为 `ssh -J`，支持多级：

```yaml
machine:
  jump: hk            # 所有任务默认经 hk 中转

tasks:
  inner:
    ssh: prod
    jump: [hk, hk2]   # 任务级覆盖：hk → hk2 → prod
  direct:
    ssh: prod
    jump: []          # 显式直连，忽略 machine.jump
```

同时自动启用：

- 连接复用 `ControlMaster=auto ControlPersist=60s`，socket 在 `~/.ssh/cm-tmux-tasks/`，避免多跳后每条命令重复握手
- `BatchMode=yes` 与 `ConnectTimeout=10`，非交互命令不会卡在密码/指纹交互上
- 有中转时 `-A` 转发 ssh-agent，供后续跳数使用本地密钥

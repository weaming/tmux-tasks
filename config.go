package main

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// JumpHosts 中转机（跳板机）链，支持 jump: hk 或 jump: [hk, hk2]
// 空数组表示显式不走中转，未配置（nil）表示继承上级配置
type JumpHosts []string

func (j *JumpHosts) UnmarshalYAML(node *yaml.Node) error {
	hosts := JumpHosts{}
	switch node.Kind {
	case yaml.ScalarNode:
		if host := strings.TrimSpace(node.Value); host != "" && host != "local" {
			hosts = append(hosts, host)
		}
	case yaml.SequenceNode:
		for _, item := range node.Content {
			if host := strings.TrimSpace(item.Value); host != "" && host != "local" {
				hosts = append(hosts, host)
			}
		}
	}
	*j = hosts
	return nil
}

type Task struct {
	Name        string            `yaml:"name"`
	Command     string            `yaml:"command"`
	Cwd         string            `yaml:"cwd"`
	Env         map[string]string `yaml:"env"`
	AutoStart   bool              `yaml:"autoStart"`
	Description string            `yaml:"description"`
	SSH         *string           `yaml:"ssh"`
	Jump        JumpHosts         `yaml:"jump"`
	Disabled    bool              `yaml:"disabled"`
}

type Machine struct {
	SSH  string    `yaml:"ssh"`
	Jump JumpHosts `yaml:"jump"`
}

type Config struct {
	Machine     *Machine `yaml:"machine"`
	InheritPath bool     `yaml:"inheritPath"`
	Tasks       []Task   `yaml:"-"`
	// map for quick lookup
	taskMap map[string]Task
}

func (c *Config) GetTask(name string) (Task, bool) {
	task, ok := c.taskMap[name]
	return task, ok
}

func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	// Use yaml.Node to preserve order
	var node yaml.Node
	if err := yaml.Unmarshal(data, &node); err != nil {
		return nil, err
	}

	cfg := &Config{
		taskMap: make(map[string]Task),
	}

	// Parse the yaml node
	for i := 0; i < len(node.Content); i++ {
		value := node.Content[i]
		if value.Kind == yaml.MappingNode {
			for j := 0; j < len(value.Content); j += 2 {
				key := value.Content[j].Value
				val := value.Content[j+1]

				switch key {
				case "machine":
					var machine Machine
					if err := val.Decode(&machine); err != nil {
						return nil, fmt.Errorf("解析 machine 失败: %w", err)
					}
					cfg.Machine = &machine
				case "inheritPath":
					cfg.InheritPath = val.Value == "true"
				case "tasks":
					if val.Kind == yaml.MappingNode {
						for k := 0; k < len(val.Content); k += 2 {
							taskName := val.Content[k].Value
							var task Task
							if err := val.Content[k+1].Decode(&task); err != nil {
								return nil, fmt.Errorf("解析任务 %s 失败: %w", taskName, err)
							}
							if task.Name == "" {
								task.Name = taskName
							}
							cfg.Tasks = append(cfg.Tasks, task)
							cfg.taskMap[taskName] = task
						}
					}
				}
			}
		}
	}

	if cfg.Machine == nil {
		cfg.Machine = &Machine{}
	}

	return cfg, nil
}

func GetConfigPath() string {
	if path := os.Getenv("TMUX_TASKS"); path != "" {
		return path
	}
	home := os.Getenv("HOME")
	return home + "/.config/tmux-tasks.yaml"
}

package main

import (
	"os"

	"gopkg.in/yaml.v3"
)

type Task struct {
	Name        string            `yaml:"name"`
	Command     string            `yaml:"command"`
	Cwd         string            `yaml:"cwd"`
	Env         map[string]string `yaml:"env"`
	AutoStart   bool              `yaml:"autoStart"`
	Description string            `yaml:"description"`
	SSH         *string           `yaml:"ssh"`
	Disabled    bool              `yaml:"disabled"`
}

type Machine struct {
	SSH string `yaml:"ssh"`
}

type Config struct {
	Machine *Machine `yaml:"machine"`
	Tasks   []Task   `yaml:"-"`
	// map for quick lookup
	taskMap map[string]Task
}

func (c *Config) GetTask(name string) (Task, bool) {
	task, ok := c.taskMap[name]
	return task, ok
}

func (c *Config) GetAllTasks() []Task {
	return c.Tasks
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
					if b, err := yaml.Marshal(val); err == nil {
						yaml.Unmarshal(b, &machine)
					}
					cfg.Machine = &machine
				case "tasks":
					if val.Kind == yaml.MappingNode {
						for k := 0; k < len(val.Content); k += 2 {
							taskName := val.Content[k].Value
							var task Task
							if b, err := yaml.Marshal(val.Content[k+1]); err == nil {
								yaml.Unmarshal(b, &task)
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

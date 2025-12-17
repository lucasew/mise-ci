package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server ServerConfig `yaml:"server" mapstructure:"server"`
	JWT    JWTConfig    `yaml:"jwt" mapstructure:"jwt"`
	GitHub GitHubConfig `yaml:"github" mapstructure:"github"`
	Nomad  NomadConfig  `yaml:"nomad" mapstructure:"nomad"`
}

type ServerConfig struct {
	HTTPAddr  string `yaml:"http_addr" mapstructure:"http_addr"`
	PublicURL string `yaml:"public_url" mapstructure:"public_url"`
}

type JWTConfig struct {
	Secret string `yaml:"secret" mapstructure:"secret"`
}

type GitHubConfig struct {
	AppID         int64  `yaml:"app_id" mapstructure:"app_id"`
	PrivateKey    string `yaml:"private_key" mapstructure:"private_key"`
	WebhookSecret string `yaml:"webhook_secret" mapstructure:"webhook_secret"`
}

type NomadConfig struct {
	Addr         string `yaml:"addr" mapstructure:"addr"`
	JobName      string `yaml:"job_name" mapstructure:"job_name"`
	DefaultImage string `yaml:"default_image" mapstructure:"default_image"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	return &cfg, nil
}

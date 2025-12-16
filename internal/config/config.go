package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server ServerConfig `yaml:"server"`
	JWT    JWTConfig    `yaml:"jwt"`
	Forge  ForgeConfig  `yaml:"forge"`
	Runner RunnerConfig `yaml:"runner"`
}

type ServerConfig struct {
	HTTPAddr  string `yaml:"http_addr"`
	GRPCAddr  string `yaml:"grpc_addr"`
	PublicURL string `yaml:"public_url"`
}

type JWTConfig struct {
	Secret string `yaml:"secret"`
}

type ForgeConfig struct {
	Type          string `yaml:"type"`
	AppID         int64  `yaml:"app_id"`
	PrivateKey    string `yaml:"private_key"`
	WebhookSecret string `yaml:"webhook_secret"`
}

type RunnerConfig struct {
	Type         string `yaml:"type"`
	Addr         string `yaml:"addr"`
	JobTemplate  string `yaml:"job_template"`
	DefaultImage string `yaml:"default_image"`
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

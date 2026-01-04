package config

import (
	"fmt"
	"strings"

	"github.com/spf13/viper"
)

type Config struct {
	Server   ServerConfig   `mapstructure:"server"`
	JWT      JWTConfig      `mapstructure:"jwt"`
	GitHub   GitHubConfig   `mapstructure:"github"`
	Nomad    NomadConfig    `mapstructure:"nomad"`
	Auth     AuthConfig     `mapstructure:"auth"`
	Storage  StorageConfig  `mapstructure:"storage"`
	Database DatabaseConfig `mapstructure:"database"`
}

type AuthConfig struct {
	AdminUsername string `mapstructure:"admin_username"`
	AdminPassword string `mapstructure:"admin_password"`
}

type ServerConfig struct {
	HTTPAddr  string `mapstructure:"http_addr"`
	PublicURL string `mapstructure:"public_url"`
}

type JWTConfig struct {
	Secret string `mapstructure:"secret"`
}

type GitHubConfig struct {
	AppID         int64  `mapstructure:"app_id"`
	PrivateKey    string `mapstructure:"private_key"`
	WebhookSecret string `mapstructure:"webhook_secret"`
}

type NomadConfig struct {
	Addr         string `mapstructure:"addr"`
	JobName      string `mapstructure:"job_name"`
	DefaultImage string `mapstructure:"default_image"`
}

type StorageConfig struct {
	DataDir string `mapstructure:"data_dir"`
}

type DatabaseConfig struct {
	Driver string `mapstructure:"driver"` // "sqlite" or "postgres"
	DSN    string `mapstructure:"dsn"`    // Connection string (path for sqlite, URL for postgres)
}

func Load(path string) (*Config, error) {
	v := viper.New()

	// Set defaults
	v.SetDefault("server.http_addr", ":8080")
	v.SetDefault("nomad.addr", "http://127.0.0.1:4646")
	v.SetDefault("nomad.job_name", "mise-ci-worker")
	v.SetDefault("nomad.default_image", "ghcr.io/mise-ci/worker:latest")
	v.SetDefault("storage.data_dir", "./data/artifacts")
	v.SetDefault("database.driver", "sqlite")
	v.SetDefault("database.dsn", "mise-ci.db")

	// Config file settings
	if path != "" {
		v.SetConfigFile(path)
		if err := v.ReadInConfig(); err != nil {
			return nil, fmt.Errorf("read config file: %w", err)
		}
	}

	// Environment variable settings
	v.SetEnvPrefix("MISE_CI")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}

	if cfg.JWT.Secret == "" {
		return nil, fmt.Errorf("jwt.secret is required")
	}

	// Validate that if one of auth config is provided, the other one is also provided
	if (cfg.Auth.AdminUsername != "") != (cfg.Auth.AdminPassword != "") {
		return nil, fmt.Errorf("auth.admin_username and auth.admin_password must be configured together")
	}

	return &cfg, nil
}

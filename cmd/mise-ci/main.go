package main

import (
	"log/slog"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	cfgFile string
	logger  *slog.Logger
)

var rootCmd = &cobra.Command{
	Use:   "mise-ci",
	Short: "Minimalist CI system",
}

func main() {
	logger = slog.New(slog.NewTextHandler(os.Stderr, nil))
	if err := rootCmd.Execute(); err != nil {
		logger.Error("execution failed", "error", err)
		os.Exit(1)
	}
}

func init() {
	cobra.OnInitialize(initConfig)
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is ./mise-ci.yaml)")
}

func initConfig() {
	if cfgFile != "" {
		viper.SetConfigFile(cfgFile)
	} else {
		viper.AddConfigPath(".")
		viper.SetConfigName("mise-ci")
		viper.SetConfigType("yaml")
	}

	viper.SetEnvPrefix("MISE_CI")
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	viper.AutomaticEnv()

	// Explicitly bind environment variables
	envVars := []string{
		"jwt.secret",
		"server.http_addr",
		"server.public_url",
		"github.app_id",
		"github.private_key",
		"github.webhook_secret",
		"nomad.addr",
		"nomad.job_name",
		"nomad.default_image",
		"auth.admin_username",
		"auth.admin_password",
		"database.driver",
		"database.dsn",
	}
	for _, key := range envVars {
		_ = viper.BindEnv(key)
	}

	if err := viper.ReadInConfig(); err == nil {
		logger.Info("using config file", "file", viper.ConfigFileUsed())
	}
}

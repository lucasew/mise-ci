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
	_ = viper.BindEnv("jwt.secret")
	_ = viper.BindEnv("server.http_addr")
	_ = viper.BindEnv("server.public_url")
	_ = viper.BindEnv("github.app_id")
	_ = viper.BindEnv("github.private_key")
	_ = viper.BindEnv("github.webhook_secret")
	_ = viper.BindEnv("nomad.addr")
	_ = viper.BindEnv("nomad.job_name")
	_ = viper.BindEnv("nomad.default_image")
	_ = viper.BindEnv("auth.admin_username")
	_ = viper.BindEnv("auth.admin_password")
	_ = viper.BindEnv("database.driver")
	_ = viper.BindEnv("database.dsn")

	if err := viper.ReadInConfig(); err == nil {
		logger.Info("using config file", "file", viper.ConfigFileUsed())
	}
}

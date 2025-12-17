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
	viper.BindEnv("jwt.secret")
	viper.BindEnv("server.http_addr")
	viper.BindEnv("server.public_url")
	viper.BindEnv("github.app_id")
	viper.BindEnv("github.private_key")
	viper.BindEnv("github.webhook_secret")
	viper.BindEnv("nomad.addr")
	viper.BindEnv("nomad.job_name")
	viper.BindEnv("nomad.default_image")

	if err := viper.ReadInConfig(); err == nil {
		logger.Info("using config file", "file", viper.ConfigFileUsed())
	}
}

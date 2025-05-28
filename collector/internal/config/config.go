package config

import (
	"time"

	"github.com/spf13/viper"
)

type Config struct {
	Scrape  ScrapeConfig  `mapstructure:"scrape"`
	Storage StorageConfig `mapstructure:"storage"`
	API     APIConfig     `mapstructure:"api"`
}

type ScrapeConfig struct {
	Jobs []Job `mapstructure:"jobs"`
}

type Job struct {
	JobName  string        `mapstructure:"job_name"`
	Targets  []Target      `mapstructure:"targets"`
	Paths    Paths         `mapstructure:"paths"`
	Interval time.Duration `mapstructure:"interval"`
}

type Target struct {
	Host      string    `mapstructure:"host"`
	Port      int       `mapstructure:"port"`
	BasicAuth AuthCreds `mapstructure:"basic_auth"`
}

type AuthCreds struct {
	Username string `mapstructure:"username"`
	Password string `mapstructure:"password"`
}

type Paths struct {
	Metrics   string `mapstructure:"metrics"`
	Processes string `mapstructure:"processes"`
	Logs      string `mapstructure:"logs"`
}

type StorageConfig struct {
	Type          string `mapstructure:"type"`
	Directory     string `mapstructure:"directory"`
	RetentionDays int    `mapstructure:"retention_days"`
}

type APIConfig struct {
	Listen    string    `mapstructure:"listen"`
	BasicAuth AuthCreds `mapstructure:"basic_auth"`
}

func Load(path string) (*Config, error) {
	viper.SetConfigFile(path)
	viper.AutomaticEnv()
	if err := viper.ReadInConfig(); err != nil {
		return nil, err
	}
	var cfg Config
	if err := viper.Unmarshal(&cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

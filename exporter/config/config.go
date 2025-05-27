package config

import (
	"time"

	"github.com/spf13/viper"
)

type BasicAuthConfig struct {
	Enabled  bool   `mapstructure:"enabled"`
	Username string `mapstructure:"username"`
	Password string `mapstructure:"password"`
}

type ServerConfig struct {
	Listen    string          `mapstructure:"listen"`
	TLSCert   string          `mapstructure:"tls_cert"`
	TLSKey    string          `mapstructure:"tls_key"`
	BasicAuth BasicAuthConfig `mapstructure:"basic_auth"`
}

type PM2Config struct {
	SocketPath   string        `mapstructure:"socket_path"`
	PollInterval time.Duration `mapstructure:"poll_interval"`
}

type LogConfig struct {
	Paths []string `mapstructure:"paths"`
}

type Config struct {
	Server ServerConfig `mapstructure:"server"`
	PM2    PM2Config    `mapstructure:"pm2"`
	Log    LogConfig    `mapstructure:"log"`
}

// Load reads the specified config file into Config
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
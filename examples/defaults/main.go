package main

import (
	"context"
	"fmt"
	"log"
	"time"

	vltconfig "github.com/tofuurem/vault-go-config"
)

type HTTPConfig struct {
	Address      string        `json:"address"`
	ReadTimeout  time.Duration `json:"read_timeout"`
	WriteTimeout time.Duration `json:"write_timeout"`
}

type Config struct {
	HTTP     HTTPConfig `json:"http"`
	LogLevel string     `json:"log_level"`
}

func defaultConfig() Config {
	return Config{
		HTTP: HTTPConfig{
			Address:      ":8080",
			ReadTimeout:  5 * time.Second,
			WriteTimeout: 10 * time.Second,
		},
		LogLevel: "info",
	}
}

func main() {
	configuration := defaultConfig()
	if err := vltconfig.LoadInto(
		context.Background(),
		&configuration,
		vltconfig.WithJSONFile("examples/defaults/config.json"),
	); err != nil {
		log.Fatal(err)
	}

	fmt.Printf(
		"listen=%s read=%s write=%s level=%s\n",
		configuration.HTTP.Address,
		configuration.HTTP.ReadTimeout,
		configuration.HTTP.WriteTimeout,
		configuration.LogLevel,
	)
}

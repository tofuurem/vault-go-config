package main

import (
	"context"
	"fmt"
	"log"

	vltconfig "github.com/tofuurem/vault-go-config"
)

type PoolConfig struct {
	MaxOpen int `json:"max_open"`
	MaxIdle int `json:"max_idle"`
}

type DatabaseConfig struct {
	Host string     `json:"host" vlt:"required"`
	Port int        `json:"port" vlt:"required"`
	Pool PoolConfig `json:"pool"`
}

type Config struct {
	Database DatabaseConfig `json:"db"`
}

func main() {
	loader, err := vltconfig.New(
		vltconfig.WithJSONFile("examples/nested/config.json"),
	)
	if err != nil {
		log.Fatal(err)
	}

	var configuration Config
	if err := loader.Load(context.Background(), &configuration); err != nil {
		log.Fatal(err)
	}

	// DB__HOST or DB__POOL__MAX_OPEN can override individual JSON leaves.
	fmt.Printf(
		"database=%s:%d pool=%d/%d\n",
		configuration.Database.Host,
		configuration.Database.Port,
		configuration.Database.Pool.MaxIdle,
		configuration.Database.Pool.MaxOpen,
	)
}

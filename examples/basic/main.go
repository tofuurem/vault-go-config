package main

import (
	"context"
	"errors"
	"fmt"
	"log"

	vltconfig "github.com/tofuurem/vault-go-config"
)

type Config struct {
	ServiceName string `json:"service_name" vlt:"required"`
	Port        int    `json:"port" vlt:"required"`
	Debug       bool   `json:"debug"`
}

func (configuration *Config) Validate() error {
	if configuration.Port < 1 || configuration.Port > 65535 {
		return errors.New("port is outside the allowed range")
	}
	return nil
}

func main() {
	configuration, err := vltconfig.Load[Config](
		context.Background(),
		vltconfig.WithJSONFile("examples/basic/config.json"),
	)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf(
		"loaded %s on port %d (debug=%t)\n",
		configuration.ServiceName,
		configuration.Port,
		configuration.Debug,
	)
}

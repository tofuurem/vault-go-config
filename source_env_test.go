package vltconfig

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestEnvLoadsAggregateAndMoreSpecificNestedLeaf(t *testing.T) {
	t.Parallel()

	type databaseConfig struct {
		Host string `json:"host"`
		Port int    `json:"port"`
	}
	type appConfig struct {
		Database databaseConfig `json:"db"`
		Debug    bool           `json:"debug"`
	}
	environment := map[string]string{
		"DB":       `{"host":"aggregate-host","port":5432}`,
		"DB__HOST": "leaf-host",
		"DEBUG":    "false",
	}
	loader := newLoaderWithSources(environmentSource{lookup: mapLookup(environment)})

	var configuration appConfig
	if err := loader.Load(context.Background(), &configuration); err != nil {
		t.Fatalf("load environment: %v", err)
	}
	want := appConfig{
		Database: databaseConfig{Host: "leaf-host", Port: 5432},
		Debug:    false,
	}
	if !reflect.DeepEqual(configuration, want) {
		t.Fatalf("configuration = %#v, want %#v", configuration, want)
	}
	if configuration.Database.Port != 5432 {
		t.Fatalf("JSON number was not decoded exactly: %d", configuration.Database.Port)
	}
}

func TestEnvUsesExactOverrideAndDisablesSubtree(t *testing.T) {
	t.Parallel()

	type hiddenConfig struct {
		Value string `json:"value"`
	}
	type appConfig struct {
		Token  string       `json:"token" env:"APP_TOKEN"`
		Hidden hiddenConfig `json:"hidden" env:"-"`
	}
	environment := map[string]string{
		"APP_TOKEN":     "fake-token",
		"TOKEN":         "ignored-token",
		"HIDDEN__VALUE": "ignored-hidden",
	}
	configuration := appConfig{Hidden: hiddenConfig{Value: "default-hidden"}}
	loader := newLoaderWithSources(environmentSource{lookup: mapLookup(environment)})

	if err := loader.Load(context.Background(), &configuration); err != nil {
		t.Fatalf("load environment: %v", err)
	}
	if configuration.Token != "fake-token" {
		t.Fatalf("token = %q", configuration.Token)
	}
	if configuration.Hidden.Value != "default-hidden" {
		t.Fatalf("disabled subtree changed: %q", configuration.Hidden.Value)
	}
}

func TestEnvPreservesEmptyRequiredStringPresence(t *testing.T) {
	t.Parallel()

	type appConfig struct {
		Name string `json:"name" vlt:"required"`
	}
	loader := newLoaderWithSources(environmentSource{lookup: mapLookup(map[string]string{"NAME": ""})})
	var configuration appConfig

	if err := loader.Load(context.Background(), &configuration); err != nil {
		t.Fatalf("load empty required string: %v", err)
	}
}

func TestEnvRejectsInvalidScalarWithoutLeakingValue(t *testing.T) {
	t.Parallel()

	type appConfig struct {
		Port int `json:"port"`
	}
	const canary = "invalid-port-secret-canary"
	loader := newLoaderWithSources(environmentSource{lookup: mapLookup(map[string]string{"PORT": canary})})
	var configuration appConfig

	err := loader.Load(context.Background(), &configuration)
	if !errors.Is(err, ErrDecode) {
		t.Fatalf("error = %v, want decode error", err)
	}
	var decodeError *DecodeError
	if !errors.As(err, &decodeError) {
		t.Fatalf("error type = %T", err)
	}
	if decodeError.Source != "environment" || decodeError.Path != "port" || decodeError.InputName != "PORT" {
		t.Fatalf("decode error = %#v", decodeError)
	}
	if strings.Contains(err.Error(), canary) {
		t.Fatalf("error leaked environment value: %v", err)
	}
}

func TestEnvRejectsMalformedCompositeJSON(t *testing.T) {
	t.Parallel()

	type appConfig struct {
		Workers []int `json:"workers"`
	}
	loader := newLoaderWithSources(environmentSource{lookup: mapLookup(map[string]string{
		"WORKERS": `[1,`,
	})})
	var configuration appConfig

	err := loader.Load(context.Background(), &configuration)
	if !errors.Is(err, ErrDecode) {
		t.Fatalf("error = %v, want decode error", err)
	}
}

func TestEnvRejectsNonObjectForStructuredField(t *testing.T) {
	t.Parallel()

	type nested struct {
		Value string `json:"value"`
	}
	type appConfig struct {
		Nested nested `json:"nested"`
	}
	loader := newLoaderWithSources(environmentSource{lookup: mapLookup(map[string]string{
		"NESTED": `[]`,
	})})
	var configuration appConfig

	err := loader.Load(context.Background(), &configuration)
	if !errors.Is(err, ErrDecode) {
		t.Fatalf("error = %v, want decode error", err)
	}
}

func TestPublicLoadReadsProcessEnvironment(t *testing.T) {
	t.Setenv("VLTCONFIG_TEST_ENDPOINT", "http://localhost:8080")

	type appConfig struct {
		Endpoint string `json:"endpoint" env:"VLTCONFIG_TEST_ENDPOINT" vlt:"required"`
	}
	configuration, err := Load[appConfig](context.Background())
	if err != nil {
		t.Fatalf("public load: %v", err)
	}
	if configuration.Endpoint != "http://localhost:8080" {
		t.Fatalf("endpoint = %q", configuration.Endpoint)
	}
}

func TestEnvLookupIsCaseSensitive(t *testing.T) {
	t.Parallel()

	type appConfig struct {
		Value string `json:"value"`
	}
	loader := newLoaderWithSources(environmentSource{lookup: mapLookup(map[string]string{"value": "lower"})})
	configuration := appConfig{Value: "default"}

	if err := loader.Load(context.Background(), &configuration); err != nil {
		t.Fatalf("load environment: %v", err)
	}
	if configuration.Value != "default" {
		t.Fatalf("value = %q, want default", configuration.Value)
	}
}

func mapLookup(values map[string]string) func(string) (string, bool) {
	return func(name string) (string, bool) {
		value, ok := values[name]
		return value, ok
	}
}

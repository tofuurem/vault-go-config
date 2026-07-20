package vltconfig

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"
)

type staticSource struct {
	sourceName string
	data       map[string]any
	err        error
}

func (source staticSource) name() string {
	return source.sourceName
}

func (source staticSource) load(context.Context, *compiledSchema) (map[string]any, error) {
	return cloneStringMap(source.data), source.err
}

func TestLoadMergesNestedSourcesByPriority(t *testing.T) {
	t.Parallel()

	type databaseConfig struct {
		Host    string            `json:"host"`
		Port    int               `json:"port"`
		Options map[string]string `json:"options"`
	}
	type appConfig struct {
		Database databaseConfig `json:"db"`
		Workers  []int          `json:"workers"`
	}

	configuration := appConfig{
		Database: databaseConfig{
			Host:    "default-host",
			Port:    5432,
			Options: map[string]string{"sslmode": "disable", "pool": "small"},
		},
		Workers: []int{1, 2},
	}
	loader := newLoaderWithSources(
		staticSource{sourceName: "low", data: map[string]any{
			"db": map[string]any{
				"host":    "json-host",
				"options": map[string]any{"sslmode": "require"},
			},
			"workers": []any{3},
		}},
		staticSource{sourceName: "high", data: map[string]any{
			"db": map[string]any{"host": "env-host"},
		}},
	)

	if err := loader.Load(context.Background(), &configuration); err != nil {
		t.Fatalf("load: %v", err)
	}

	want := appConfig{
		Database: databaseConfig{
			Host:    "env-host",
			Port:    5432,
			Options: map[string]string{"sslmode": "require", "pool": "small"},
		},
		Workers: []int{3},
	}
	if !reflect.DeepEqual(configuration, want) {
		t.Fatalf("configuration = %#v, want %#v", configuration, want)
	}
}

func TestLoadIntoDoesNotMutateDefaultsOnFailure(t *testing.T) {
	t.Parallel()

	type appConfig struct {
		Values map[string][]int `json:"values"`
		Port   int              `json:"port"`
	}
	configuration := appConfig{
		Values: map[string][]int{"stable": {1, 2}},
		Port:   5432,
	}
	before := appConfig{
		Values: map[string][]int{"stable": {1, 2}},
		Port:   5432,
	}
	loader := newLoaderWithSources(staticSource{sourceName: "test", data: map[string]any{
		"values": map[string]any{"stable": []any{9}},
		"port":   "not-an-int",
	}})

	err := loader.Load(context.Background(), &configuration)
	if !errors.Is(err, ErrDecode) {
		t.Fatalf("error = %v, want decode error", err)
	}
	if !reflect.DeepEqual(configuration, before) {
		t.Fatalf("destination mutated on failure: %#v", configuration)
	}
}

func TestRequiredUsesExternalPresenceNotZeroValue(t *testing.T) {
	t.Parallel()

	type appConfig struct {
		Enabled bool   `json:"enabled" vlt:"required"`
		Count   int    `json:"count" vlt:"required"`
		Name    string `json:"name" vlt:"required"`
	}
	loader := newLoaderWithSources(staticSource{sourceName: "test", data: map[string]any{
		"enabled": false,
		"count":   0,
		"name":    "",
	}})

	var configuration appConfig
	if err := loader.Load(context.Background(), &configuration); err != nil {
		t.Fatalf("load explicit zero values: %v", err)
	}
}

func TestRequiredIgnoresProgrammaticDefault(t *testing.T) {
	t.Parallel()

	type appConfig struct {
		Token string `json:"token" vlt:"required"`
	}
	configuration := appConfig{Token: "default-must-not-satisfy-required"}

	err := newLoaderWithSources().Load(context.Background(), &configuration)
	if !errors.Is(err, ErrRequired) {
		t.Fatalf("error = %v, want required error", err)
	}
	var required *RequiredError
	if !errors.As(err, &required) || !reflect.DeepEqual(required.Paths, []string{"token"}) {
		t.Fatalf("required error = %#v", required)
	}
}

func TestRequiredNestedPointerActivatesOnlyWhenSectionIsPresent(t *testing.T) {
	t.Parallel()

	type databaseConfig struct {
		Host string `json:"host" vlt:"required"`
	}
	type appConfig struct {
		Database *databaseConfig `json:"db"`
	}

	var absent appConfig
	if err := newLoaderWithSources().Load(context.Background(), &absent); err != nil {
		t.Fatalf("optional absent section: %v", err)
	}

	var present appConfig
	err := newLoaderWithSources(staticSource{data: map[string]any{"db": map[string]any{}}}).Load(
		context.Background(),
		&present,
	)
	if !errors.Is(err, ErrRequired) {
		t.Fatalf("error = %v, want nested required error", err)
	}
}

func TestLoadSupportsExplicitNullForPointer(t *testing.T) {
	t.Parallel()

	type databaseConfig struct {
		Host string `json:"host"`
	}
	type appConfig struct {
		Database *databaseConfig `json:"db"`
	}
	configuration := appConfig{Database: &databaseConfig{Host: "default"}}
	loader := newLoaderWithSources(staticSource{data: map[string]any{"db": nil}})

	if err := loader.Load(context.Background(), &configuration); err != nil {
		t.Fatalf("load null pointer: %v", err)
	}
	if configuration.Database != nil {
		t.Fatalf("database = %#v, want nil", configuration.Database)
	}
}

func TestLoadRejectsNullForScalar(t *testing.T) {
	t.Parallel()

	type appConfig struct {
		Port int `json:"port"`
	}
	var configuration appConfig

	err := newLoaderWithSources(staticSource{data: map[string]any{"port": nil}}).Load(
		context.Background(),
		&configuration,
	)
	if !errors.Is(err, ErrDecode) {
		t.Fatalf("error = %v, want decode error", err)
	}
}

func TestLoadRejectsUnknownFields(t *testing.T) {
	t.Parallel()

	type appConfig struct {
		Known string `json:"known"`
	}
	var configuration appConfig

	err := newLoaderWithSources(staticSource{data: map[string]any{"unknown": "canary-value"}}).Load(
		context.Background(),
		&configuration,
	)
	if !errors.Is(err, ErrDecode) {
		t.Fatalf("error = %v, want decode error", err)
	}
	if containsText(err.Error(), "canary-value") {
		t.Fatalf("error leaked input value: %v", err)
	}
}

type validatingConfig struct {
	Port int `json:"port"`
}

func (configuration *validatingConfig) Validate() error {
	if configuration.Port < 1 {
		return fmt.Errorf("port value %d is invalid", configuration.Port)
	}
	return nil
}

func TestLoadCallsCustomValidationWithoutLeakingItsMessage(t *testing.T) {
	t.Parallel()

	configuration := validatingConfig{Port: -987654}
	err := newLoaderWithSources().Load(context.Background(), &configuration)
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("error = %v, want validation error", err)
	}
	if containsText(err.Error(), "-987654") {
		t.Fatalf("outer error leaked validation value: %v", err)
	}
	if configuration.Port != -987654 {
		t.Fatalf("destination changed on validation failure: %d", configuration.Port)
	}
}

func TestLoadSupportsDurationAndTextUnmarshaler(t *testing.T) {
	t.Parallel()

	type appConfig struct {
		Timeout time.Duration `json:"timeout"`
	}
	var configuration appConfig
	loader := newLoaderWithSources(staticSource{data: map[string]any{"timeout": "5s"}})

	if err := loader.Load(context.Background(), &configuration); err != nil {
		t.Fatalf("load duration: %v", err)
	}
	if configuration.Timeout != 5*time.Second {
		t.Fatalf("timeout = %s, want 5s", configuration.Timeout)
	}
}

func TestLoadGenericRejectsPointerType(t *testing.T) {
	t.Parallel()

	_, err := Load[*validatingConfig](context.Background())
	if !errors.Is(err, ErrSchema) {
		t.Fatalf("error = %v, want schema error", err)
	}
}

func TestLoaderRejectsInvalidDestinationAndContext(t *testing.T) {
	t.Parallel()

	loader := newLoaderWithSources()
	if err := loader.Load(context.Background(), nil); !errors.Is(err, ErrSchema) {
		t.Fatalf("nil destination error = %v", err)
	}
	value := 1
	if err := loader.Load(context.Background(), &value); !errors.Is(err, ErrSchema) {
		t.Fatalf("scalar destination error = %v", err)
	}
}

func containsText(value, fragment string) bool {
	return len(fragment) > 0 && len(value) >= len(fragment) && reflect.ValueOf(value).String() != "" &&
		findSubstring(value, fragment)
}

func findSubstring(value, fragment string) bool {
	for index := 0; index+len(fragment) <= len(value); index++ {
		if value[index:index+len(fragment)] == fragment {
			return true
		}
	}
	return false
}

package vltconfig

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func TestJSONLoadsNestedCollectionsAndExactNumbers(t *testing.T) {
	t.Parallel()

	path := writeJSONFixture(t, `{
		"db":{"host":"json-host","port":5432},
		"workers":[1,2],
		"labels":{"region":"local"}
	}`)
	type databaseConfig struct {
		Host string `json:"host"`
		Port int64  `json:"port"`
	}
	type appConfig struct {
		Database databaseConfig    `json:"db"`
		Workers  []int             `json:"workers"`
		Labels   map[string]string `json:"labels"`
	}
	var configuration appConfig

	if err := LoadInto(context.Background(), &configuration, WithJSONFile(path)); err != nil {
		t.Fatalf("load JSON: %v", err)
	}
	want := appConfig{
		Database: databaseConfig{Host: "json-host", Port: 5432},
		Workers:  []int{1, 2},
		Labels:   map[string]string{"region": "local"},
	}
	if !reflect.DeepEqual(configuration, want) {
		t.Fatalf("configuration = %#v, want %#v", configuration, want)
	}
}

func TestJSONAndEnvironmentOverrideDefaultsPerLeaf(t *testing.T) {
	path := writeJSONFixture(t, `{"db":{"host":"json-host","port":6432}}`)
	t.Setenv("VLTCONFIG_DATABASE_HOST", "env-host")

	type databaseConfig struct {
		Host string `json:"host" env:"VLTCONFIG_DATABASE_HOST"`
		Port int    `json:"port"`
		SSL  bool   `json:"ssl"`
	}
	type appConfig struct {
		Database databaseConfig `json:"db"`
	}
	configuration := appConfig{Database: databaseConfig{Host: "default", Port: 5432, SSL: true}}

	if err := LoadInto(context.Background(), &configuration, WithJSONFile(path)); err != nil {
		t.Fatalf("load sources: %v", err)
	}
	want := appConfig{Database: databaseConfig{Host: "env-host", Port: 6432, SSL: true}}
	if !reflect.DeepEqual(configuration, want) {
		t.Fatalf("configuration = %#v, want %#v", configuration, want)
	}
}

func TestJSONUsesDirectoryEnvironment(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "config.json")
	if err := os.WriteFile(path, []byte(`{"value":"from-directory"}`), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	t.Setenv(JSONPathEnvironment, directory)

	type appConfig struct {
		Value string `json:"value"`
	}
	configuration, err := Load[appConfig](context.Background())
	if err != nil {
		t.Fatalf("load directory JSON: %v", err)
	}
	if configuration.Value != "from-directory" {
		t.Fatalf("value = %q", configuration.Value)
	}
}

func TestJSONExplicitFileTakesPrecedenceOverDirectoryEnvironment(t *testing.T) {
	explicit := writeJSONFixture(t, `{"value":"explicit"}`)
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "config.json"), []byte(`{"value":"directory"}`), 0o600); err != nil {
		t.Fatalf("write directory fixture: %v", err)
	}
	t.Setenv(JSONPathEnvironment, directory)

	type appConfig struct {
		Value string `json:"value"`
	}
	var configuration appConfig
	if err := LoadInto(context.Background(), &configuration, WithJSONFile(explicit)); err != nil {
		t.Fatalf("load explicit JSON: %v", err)
	}
	if configuration.Value != "explicit" {
		t.Fatalf("value = %q", configuration.Value)
	}
}

func TestJSONMissingImplicitFileSkips(t *testing.T) {
	t.Parallel()

	source := jsonSource{lookup: mapLookup(nil)}
	data, err := source.load(context.Background(), nil)
	if err != nil {
		t.Fatalf("implicit missing file: %v", err)
	}
	if len(data) != 0 {
		t.Fatalf("data = %#v, want empty", data)
	}
}

func TestJSONMissingExplicitFileFails(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "missing.json")
	source := jsonSource{lookup: mapLookup(nil), explicitFile: path}
	_, err := source.load(context.Background(), nil)
	if !errors.Is(err, ErrJSONNotFound) || !errors.Is(err, ErrJSON) || !errors.Is(err, ErrConfig) {
		t.Fatalf("error = %v, want JSON not-found hierarchy", err)
	}
}

func TestJSONRejectsMalformedDocumentsWithoutLeakingValues(t *testing.T) {
	t.Parallel()

	fixtures := map[string][]byte{
		"syntax":   []byte(`{"secret_canary":`),
		"trailing": []byte(`{} {"secret_canary":"hidden"}`),
		"utf8":     {0xff, 0xfe},
	}
	for name, document := range fixtures {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "config.json")
			if err := os.WriteFile(path, document, 0o600); err != nil {
				t.Fatalf("write fixture: %v", err)
			}
			_, err := (jsonSource{explicitFile: path, lookup: mapLookup(nil)}).load(context.Background(), nil)
			if !errors.Is(err, ErrJSONMalformed) {
				t.Fatalf("error = %v, want malformed JSON", err)
			}
			if strings.Contains(err.Error(), "secret_canary") || strings.Contains(err.Error(), "hidden") {
				t.Fatalf("error leaked document data: %v", err)
			}
		})
	}
}

func TestJSONRejectsNonObjectRoots(t *testing.T) {
	t.Parallel()

	for name, document := range map[string]string{
		"array":  `[]`,
		"null":   `null`,
		"string": `"value"`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			path := writeJSONFixture(t, document)
			_, err := (jsonSource{explicitFile: path, lookup: mapLookup(nil)}).load(context.Background(), nil)
			if !errors.Is(err, ErrJSONStructure) {
				t.Fatalf("error = %v, want JSON structure error", err)
			}
		})
	}
}

func TestJSONPermissionFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX file permissions are required")
	}
	path := writeJSONFixture(t, `{}`)
	if err := os.Chmod(path, 0); err != nil {
		t.Fatalf("chmod fixture: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o600) })

	_, err := (jsonSource{explicitFile: path, lookup: mapLookup(nil)}).load(context.Background(), nil)
	if !errors.Is(err, ErrJSONPermission) {
		t.Fatalf("error = %v, want permission error", err)
	}
}

func TestJSONRejectsEmptyConfiguredPaths(t *testing.T) {
	t.Parallel()

	type appConfig struct{}
	var configuration appConfig
	if err := LoadInto(context.Background(), &configuration, WithJSONFile(" ")); !errors.Is(err, ErrJSON) {
		t.Fatalf("empty option error = %v", err)
	}
	_, err := (jsonSource{lookup: mapLookup(map[string]string{JSONPathEnvironment: ""})}).load(
		context.Background(),
		nil,
	)
	if !errors.Is(err, ErrJSON) {
		t.Fatalf("empty environment path error = %v", err)
	}
}

func writeJSONFixture(t *testing.T, document string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(document), 0o600); err != nil {
		t.Fatalf("write JSON fixture: %v", err)
	}
	return path
}

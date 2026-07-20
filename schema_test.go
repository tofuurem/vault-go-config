package vltconfig

import (
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestSchemaCompilesNestedFieldsAndTags(t *testing.T) {
	t.Parallel()

	type databaseConfig struct {
		Host     string        `json:"host" vlt:"required"`
		Timeout  time.Duration `json:"timeout"`
		Password string        `json:"password" env:"DATABASE_PASSWORD"`
		Ignored  string        `json:"-"`
	}
	type appConfig struct {
		Database *databaseConfig `json:"db" vlt:"required"`
		HTTPPort int             `json:"http_port"`
	}

	schema, err := compileSchema(reflect.TypeFor[appConfig]())
	if err != nil {
		t.Fatalf("compile schema: %v", err)
	}

	if got, want := len(schema.nodes), 5; got != want {
		t.Fatalf("node count = %d, want %d", got, want)
	}
	if got, want := len(schema.leaves), 4; got != want {
		t.Fatalf("leaf count = %d, want %d", got, want)
	}

	assertSchemaField(t, schema, "db", "DB", true, true)
	assertSchemaField(t, schema, "db.host", "DB__HOST", true, false)
	assertSchemaField(t, schema, "db.timeout", "DB__TIMEOUT", false, false)
	assertSchemaField(t, schema, "db.password", "DATABASE_PASSWORD", false, false)
	assertSchemaField(t, schema, "http_port", "HTTP_PORT", false, false)
}

func TestSchemaInfersSnakeCaseForAcronyms(t *testing.T) {
	t.Parallel()

	type config struct {
		DatabaseURL string
		HTTPServer  string
		MyID        string
	}

	schema, err := compileSchema(reflect.TypeFor[config]())
	if err != nil {
		t.Fatalf("compile schema: %v", err)
	}

	assertSchemaField(t, schema, "database_url", "DATABASE_URL", false, false)
	assertSchemaField(t, schema, "http_server", "HTTP_SERVER", false, false)
	assertSchemaField(t, schema, "my_id", "MY_ID", false, false)
}

func TestSchemaRejectsInvalidRoots(t *testing.T) {
	t.Parallel()

	for name, root := range map[string]reflect.Type{
		"nil":     nil,
		"scalar":  reflect.TypeFor[int](),
		"pointer": reflect.TypeFor[*struct{}](),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := compileSchema(root)
			if !errors.Is(err, ErrSchema) || !errors.Is(err, ErrConfig) {
				t.Fatalf("error = %v, want schema and config categories", err)
			}
		})
	}
}

func TestSchemaRejectsRecursiveStruct(t *testing.T) {
	t.Parallel()

	type recursive struct {
		Next *recursive `json:"next"`
	}

	_, err := compileSchema(reflect.TypeFor[recursive]())
	if !errors.Is(err, ErrSchema) {
		t.Fatalf("error = %v, want schema error", err)
	}
}

func TestSchemaRejectsNonStringMapKeys(t *testing.T) {
	t.Parallel()

	type config struct {
		Values map[int]string `json:"values"`
	}

	_, err := compileSchema(reflect.TypeFor[config]())
	if !errors.Is(err, ErrSchema) {
		t.Fatalf("error = %v, want schema error", err)
	}
}

func TestSchemaRejectsCanonicalCollision(t *testing.T) {
	t.Parallel()

	root := reflect.StructOf([]reflect.StructField{
		{Name: "First", Type: reflect.TypeFor[string](), Tag: `json:"value"`},
		{Name: "Second", Type: reflect.TypeFor[string](), Tag: `json:"value"`},
	})

	_, err := compileSchema(root)
	if !errors.Is(err, ErrSchema) {
		t.Fatalf("error = %v, want schema error", err)
	}
}

func TestSchemaRejectsEnvironmentCollision(t *testing.T) {
	t.Parallel()

	type config struct {
		First  string `json:"first" env:"SHARED"`
		Second string `json:"second" env:"SHARED"`
	}

	_, err := compileSchema(reflect.TypeFor[config]())
	if !errors.Is(err, ErrSchema) {
		t.Fatalf("error = %v, want schema error", err)
	}
}

func TestSchemaRejectsUnsupportedValidationTag(t *testing.T) {
	t.Parallel()

	type config struct {
		Value string `vlt:"secret"`
	}

	_, err := compileSchema(reflect.TypeFor[config]())
	if !errors.Is(err, ErrSchema) {
		t.Fatalf("error = %v, want schema error", err)
	}
}

func TestSchemaRequiresExplicitTagForAnonymousField(t *testing.T) {
	t.Parallel()

	type Embedded struct {
		Value string
	}
	type config struct {
		Embedded
	}

	_, err := compileSchema(reflect.TypeFor[config]())
	if !errors.Is(err, ErrSchema) {
		t.Fatalf("error = %v, want schema error", err)
	}
}

func TestSchemaDisablesNestedEnvironmentSubtree(t *testing.T) {
	t.Parallel()

	type nested struct {
		Value string `json:"value"`
	}
	type config struct {
		Nested nested `json:"nested" env:"-"`
	}

	schema, err := compileSchema(reflect.TypeFor[config]())
	if err != nil {
		t.Fatalf("compile schema: %v", err)
	}
	assertSchemaField(t, schema, "nested", "", false, false)
	assertSchemaField(t, schema, "nested.value", "", false, false)
}

func assertSchemaField(
	t *testing.T,
	schema *compiledSchema,
	path string,
	envName string,
	required bool,
	pointer bool,
) {
	t.Helper()
	for _, field := range schema.nodes {
		if field.pathString() != path {
			continue
		}
		if field.envName != envName {
			t.Errorf("%s env = %q, want %q", path, field.envName, envName)
		}
		if field.required != required {
			t.Errorf("%s required = %t, want %t", path, field.required, required)
		}
		if field.pointer != pointer {
			t.Errorf("%s pointer = %t, want %t", path, field.pointer, pointer)
		}
		return
	}
	t.Fatalf("schema field %q not found", path)
}

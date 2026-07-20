package vltconfig

import (
	"context"
	"fmt"
	"reflect"
)

// Loader owns immutable source options and reusable source state. A Loader is
// safe for concurrent calls when its configured sources are safe for
// concurrent use.
type Loader struct {
	sources    []configSource
	vaultCache *vaultCache
}

// New creates a reusable configuration loader.
func New(options ...Option) (*Loader, error) {
	configuration := defaultLoaderOptions()
	for _, option := range options {
		if option == nil {
			return nil, fmt.Errorf("%w: option must not be nil", ErrConfig)
		}
		if err := option.apply(&configuration); err != nil {
			return nil, err
		}
	}
	cache, err := newVaultCache()
	if err != nil {
		return nil, fmt.Errorf("%w: initialize Vault cache", ErrConfig)
	}
	loader := &Loader{vaultCache: cache}
	loader.sources = []configSource{
		jsonSource{
			lookup:       configuration.lookupEnv,
			explicitFile: configuration.jsonFile,
		},
		vaultSource{
			lookup:  configuration.lookupEnv,
			policy:  configuration.vaultPolicy,
			factory: newAPIVaultClient,
			retry:   defaultVaultRetry(),
			cache:   cache,
		},
		environmentSource{lookup: configuration.lookupEnv},
	}
	return loader, nil
}

// Load returns a fully assembled configuration value with no programmatic
// defaults.
func Load[T any](ctx context.Context, options ...Option) (T, error) {
	var destination T
	if err := LoadInto(ctx, &destination, options...); err != nil {
		return destination, err
	}
	return destination, nil
}

// LoadInto treats destination's existing contents as the lowest-priority
// defaults and assigns the result only after complete success.
func LoadInto[T any](ctx context.Context, destination *T, options ...Option) error {
	loader, err := New(options...)
	if err != nil {
		return err
	}
	return loader.Load(ctx, destination)
}

// Load assembles configuration into a non-nil pointer to a struct.
func (loader *Loader) Load(ctx context.Context, destination any) error {
	if ctx == nil {
		return fmt.Errorf("%w: context must not be nil", ErrConfig)
	}
	if loader == nil {
		return fmt.Errorf("%w: loader must not be nil", ErrConfig)
	}

	destinationValue := reflect.ValueOf(destination)
	if !destinationValue.IsValid() || destinationValue.Kind() != reflect.Pointer || destinationValue.IsNil() {
		return &SchemaError{Problem: "destination must be a non-nil pointer to a struct"}
	}
	destinationValue = destinationValue.Elem()
	if destinationValue.Kind() != reflect.Struct {
		return &SchemaError{Problem: "destination must point to a struct"}
	}

	schema, err := compileSchema(destinationValue.Type())
	if err != nil {
		return err
	}

	external := make(map[string]any)
	for _, source := range loader.sources {
		if err := ctx.Err(); err != nil {
			return err
		}
		loaded, err := source.load(ctx, schema)
		if err != nil {
			return err
		}
		external = mergeData(external, loaded)
	}

	temporary := cloneStruct(destinationValue)
	if err := prepareDestination(temporary, schema, external); err != nil {
		return err
	}
	if err := decodeData(external, temporary.Addr().Interface()); err != nil {
		return err
	}
	if err := validateRequired(schema, external); err != nil {
		return err
	}
	if err := validateCustom(temporary.Addr().Interface()); err != nil {
		return err
	}

	destinationValue.Set(temporary)
	return nil
}

// InvalidateVaultCache removes every cached Vault payload owned by this
// Loader. It is safe to call concurrently with Load and on a nil Loader.
func (loader *Loader) InvalidateVaultCache() {
	if loader != nil && loader.vaultCache != nil {
		loader.vaultCache.invalidate()
	}
}

func newLoaderWithSources(sources ...configSource) *Loader {
	return &Loader{sources: append([]configSource(nil), sources...)}
}

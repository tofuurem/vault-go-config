package vltconfig

import (
	"os"
	"strings"
)

// Option configures a Loader. The option interface is sealed so package
// invariants cannot be bypassed by external implementations.
type Option interface {
	apply(*loaderOptions) error
}

type optionFunc func(*loaderOptions) error

func (function optionFunc) apply(options *loaderOptions) error {
	return function(options)
}

type loaderOptions struct {
	lookupEnv func(string) (string, bool)
	jsonFile  string
}

func defaultLoaderOptions() loaderOptions {
	return loaderOptions{lookupEnv: os.LookupEnv}
}

// WithJSONFile selects an exact JSON file and takes precedence over
// PYDANTIC_JSON_PATH. The file must exist and contain one JSON object.
func WithJSONFile(path string) Option {
	return optionFunc(func(options *loaderOptions) error {
		if strings.TrimSpace(path) == "" {
			return &JSONError{CauseType: "empty path", kind: ErrJSON}
		}
		options.jsonFile = path
		return nil
	})
}

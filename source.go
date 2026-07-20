package vltconfig

import "context"

type configSource interface {
	name() string
	load(context.Context, *compiledSchema) (map[string]any, error)
}

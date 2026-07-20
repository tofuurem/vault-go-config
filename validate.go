package vltconfig

import (
	"fmt"
	"sort"
)

// Validator may be implemented by a configuration struct for custom semantic
// validation after all sources have loaded and required paths have been
// checked.
type Validator interface {
	Validate() error
}

func validateRequired(schema *compiledSchema, data map[string]any) error {
	missing := make([]string, 0)
	collectMissingRequired(schema.fields, data, true, &missing)
	if len(missing) == 0 {
		return nil
	}
	sort.Strings(missing)
	return &RequiredError{Paths: missing}
}

func collectMissingRequired(
	fields []*fieldSchema,
	data map[string]any,
	active bool,
	missing *[]string,
) {
	for _, field := range fields {
		raw, present := data[field.canonicalName]
		if active && field.required && !present {
			*missing = append(*missing, field.pathString())
		}

		if !field.structured {
			continue
		}

		childActive := active
		childData := map[string]any(nil)
		if field.pointer {
			childActive = active && present && raw != nil
		}
		if nested, ok := stringMap(raw); ok {
			childData = nested
		} else if present {
			childActive = false
		}
		collectMissingRequired(field.children, childData, childActive, missing)
	}
}

func validateCustom(destination any) error {
	validator, ok := destination.(Validator)
	if !ok {
		return nil
	}
	if err := validator.Validate(); err != nil {
		return &ValidationError{CauseType: fmt.Sprintf("%T", err), cause: err}
	}
	return nil
}

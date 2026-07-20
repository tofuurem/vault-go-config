package vltconfig

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
)

type environmentSource struct {
	lookup func(string) (string, bool)
}

func (environmentSource) name() string {
	return "environment"
}

func (source environmentSource) load(
	ctx context.Context,
	schema *compiledSchema,
) (map[string]any, error) {
	result := make(map[string]any)
	for _, field := range schema.nodes {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if field.envName == "" {
			continue
		}
		raw, present := source.lookup(field.envName)
		if !present {
			continue
		}

		value, err := environmentValue(raw, field)
		if err != nil {
			return nil, err
		}
		setDataPath(result, field.path, value)
	}

	if len(result) == 0 {
		return result, nil
	}
	temporary := reflect.New(schema.root).Interface()
	if err := decodeData(result, temporary); err != nil {
		decodeError := &DecodeError{
			Source:    "environment",
			Path:      safeDecodePath(err),
			CauseType: fmt.Sprintf("%T", err),
		}
		if field := schemaFieldByPath(schema, decodeError.Path); field != nil {
			decodeError.InputName = field.envName
		}
		return nil, decodeError
	}
	return result, nil
}

func environmentValue(raw string, field *fieldSchema) (any, error) {
	if !environmentJSONType(field) {
		return raw, nil
	}
	decoder := json.NewDecoder(bytes.NewBufferString(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, &DecodeError{
			Source:    "environment",
			Path:      field.pathString(),
			InputName: field.envName,
			CauseType: fmt.Sprintf("%T", err),
		}
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, &DecodeError{
			Source:    "environment",
			Path:      field.pathString(),
			InputName: field.envName,
			CauseType: "trailing JSON data",
		}
	}
	if field.structured && value != nil {
		if _, ok := value.(map[string]any); !ok {
			return nil, &DecodeError{
				Source:    "environment",
				Path:      field.pathString(),
				InputName: field.envName,
				CauseType: "expected JSON object",
			}
		}
	}
	return value, nil
}

func environmentJSONType(field *fieldSchema) bool {
	if field.structured {
		return true
	}
	typeOf := field.typeOf
	if typeOf.Kind() == reflect.Pointer {
		typeOf = typeOf.Elem()
	}
	switch typeOf.Kind() {
	case reflect.Array, reflect.Interface, reflect.Map, reflect.Slice:
		return true
	default:
		return false
	}
}

func setDataPath(destination map[string]any, path []string, value any) {
	cursor := destination
	for _, segment := range path[:len(path)-1] {
		nested, ok := stringMap(cursor[segment])
		if !ok {
			nested = make(map[string]any)
			cursor[segment] = nested
		}
		cursor = nested
	}
	last := path[len(path)-1]
	if current, currentIsMap := stringMap(cursor[last]); currentIsMap {
		if overlay, overlayIsMap := stringMap(value); overlayIsMap {
			cursor[last] = mergeData(current, overlay)
			return
		}
	}
	cursor[last] = cloneData(value)
}

func schemaFieldByPath(schema *compiledSchema, path string) *fieldSchema {
	for _, field := range schema.nodes {
		if field.pathString() == path {
			return field
		}
	}
	return nil
}

func safeDecodePath(err error) string {
	var decodeError *DecodeError
	if errors.As(err, &decodeError) {
		return decodeError.Path
	}
	return ""
}

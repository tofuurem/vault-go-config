package vltconfig

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strconv"

	"github.com/go-viper/mapstructure/v2"
)

func decodeData(data map[string]any, destination any) error {
	metadata := &mapstructure.Metadata{}
	decoder, err := mapstructure.NewDecoder(&mapstructure.DecoderConfig{
		DecodeHook: mapstructure.ComposeDecodeHookFunc(
			jsonNumberDecodeHook(),
			mapstructure.StringToTimeDurationHookFunc(),
			mapstructure.TextUnmarshallerHookFunc(),
			mapstructure.StringToBasicTypeHookFunc(),
		),
		ErrorUnused:       true,
		ZeroFields:        false,
		WeaklyTypedInput:  true,
		Metadata:          metadata,
		Result:            destination,
		TagName:           jsonTagName,
		MapFieldName:      snakeCase,
		MatchName:         func(mapKey, fieldName string) bool { return mapKey == fieldName },
		DecodeNil:         true,
		SquashTagOption:   "squash",
		AllowUnsetPointer: true,
	})
	if err != nil {
		return &DecodeError{Source: "merged", CauseType: fmt.Sprintf("%T", err)}
	}
	if err := decoder.Decode(data); err != nil {
		path := ""
		var fieldError *mapstructure.DecodeError
		if errors.As(err, &fieldError) {
			path = fieldError.Name()
		}
		return &DecodeError{
			Source:    "merged",
			Path:      path,
			CauseType: fmt.Sprintf("%T", err),
		}
	}
	if len(metadata.Unused) > 0 {
		return &DecodeError{Source: "merged", Path: metadata.Unused[0], CauseType: "unknown field"}
	}
	return nil
}

func jsonNumberDecodeHook() mapstructure.DecodeHookFuncType {
	jsonNumberType := reflect.TypeFor[json.Number]()
	return func(from, to reflect.Type, data any) (any, error) {
		if from != jsonNumberType {
			return data, nil
		}
		number := data.(json.Number)
		target := to
		if target.Kind() == reflect.Pointer {
			target = target.Elem()
		}
		switch target.Kind() {
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			value, err := strconv.ParseInt(number.String(), 10, target.Bits())
			if err != nil {
				return nil, err
			}
			return value, nil
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			value, err := strconv.ParseUint(number.String(), 10, target.Bits())
			if err != nil {
				return nil, err
			}
			return value, nil
		case reflect.Float32, reflect.Float64:
			value, err := strconv.ParseFloat(number.String(), target.Bits())
			if err != nil {
				return nil, err
			}
			return value, nil
		case reflect.String:
			return number.String(), nil
		default:
			return data, nil
		}
	}
}

func prepareDestination(destination reflect.Value, schema *compiledSchema, data map[string]any) error {
	return prepareStruct(destination, schema.fields, data)
}

func prepareStruct(destination reflect.Value, fields []*fieldSchema, data map[string]any) error {
	for _, field := range fields {
		raw, present := data[field.canonicalName]
		if !present {
			continue
		}
		fieldValue := destination.Field(field.index[len(field.index)-1])
		if raw == nil {
			if !nullableType(field.typeOf) {
				return &DecodeError{Source: "merged", Path: field.pathString(), CauseType: "null"}
			}
			fieldValue.SetZero()
			continue
		}

		if field.structured {
			nested, ok := stringMap(raw)
			if !ok {
				continue
			}
			if fieldValue.Kind() == reflect.Pointer {
				if fieldValue.IsNil() {
					fieldValue.Set(reflect.New(fieldValue.Type().Elem()))
				}
				fieldValue = fieldValue.Elem()
			}
			if err := prepareStruct(fieldValue, field.children, nested); err != nil {
				return err
			}
			continue
		}

		switch fieldValue.Kind() {
		case reflect.Array, reflect.Slice:
			fieldValue.SetZero()
		case reflect.Pointer:
			fieldValue.SetZero()
		}
	}
	return nil
}

func nullableType(typeOf reflect.Type) bool {
	switch typeOf.Kind() {
	case reflect.Pointer, reflect.Map, reflect.Slice, reflect.Interface:
		return true
	default:
		return false
	}
}

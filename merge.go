package vltconfig

import "reflect"

func mergeData(base, overlay map[string]any) map[string]any {
	merged := cloneStringMap(base)
	for key, overlayValue := range overlay {
		baseMap, baseIsMap := stringMap(merged[key])
		overlayMap, overlayIsMap := stringMap(overlayValue)
		if baseIsMap && overlayIsMap {
			merged[key] = mergeData(baseMap, overlayMap)
			continue
		}
		merged[key] = cloneData(overlayValue)
	}
	return merged
}

func cloneStringMap(source map[string]any) map[string]any {
	if source == nil {
		return make(map[string]any)
	}
	result := make(map[string]any, len(source))
	for key, value := range source {
		result[key] = cloneData(value)
	}
	return result
}

func cloneData(value any) any {
	if value == nil {
		return nil
	}
	return genericValue(reflect.ValueOf(value))
}

func genericValue(value reflect.Value) any {
	if !value.IsValid() {
		return nil
	}
	if value.Kind() == reflect.Interface {
		if value.IsNil() {
			return nil
		}
		return genericValue(value.Elem())
	}
	if value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return nil
		}
		return genericValue(value.Elem())
	}

	switch value.Kind() {
	case reflect.Map:
		if value.IsNil() {
			return nil
		}
		if value.Type().Key().Kind() != reflect.String {
			return value.Interface()
		}
		result := make(map[string]any, value.Len())
		iterator := value.MapRange()
		for iterator.Next() {
			result[iterator.Key().String()] = genericValue(iterator.Value())
		}
		return result
	case reflect.Slice:
		if value.IsNil() {
			return nil
		}
		result := make([]any, value.Len())
		for index := range value.Len() {
			result[index] = genericValue(value.Index(index))
		}
		return result
	case reflect.Array:
		result := make([]any, value.Len())
		for index := range value.Len() {
			result[index] = genericValue(value.Index(index))
		}
		return result
	default:
		return value.Interface()
	}
}

func stringMap(value any) (map[string]any, bool) {
	if value == nil {
		return nil, false
	}
	typed, ok := value.(map[string]any)
	if ok {
		return typed, true
	}
	reflected := reflect.ValueOf(value)
	if reflected.Kind() != reflect.Map || reflected.Type().Key().Kind() != reflect.String {
		return nil, false
	}
	converted := genericValue(reflected)
	result, ok := converted.(map[string]any)
	return result, ok
}

func cloneStruct(source reflect.Value) reflect.Value {
	clone := reflect.New(source.Type()).Elem()
	clone.Set(source)
	cloneExportedFields(clone, source)
	return clone
}

func cloneExportedFields(destination, source reflect.Value) {
	for index := range source.NumField() {
		fieldType := source.Type().Field(index)
		if fieldType.PkgPath != "" {
			continue
		}
		destination.Field(index).Set(cloneTypedValue(source.Field(index)))
	}
}

func cloneTypedValue(source reflect.Value) reflect.Value {
	if !source.IsValid() {
		return source
	}

	switch source.Kind() {
	case reflect.Interface:
		if source.IsNil() {
			return reflect.Zero(source.Type())
		}
		cloned := cloneTypedValue(source.Elem())
		result := reflect.New(source.Type()).Elem()
		result.Set(cloned)
		return result
	case reflect.Pointer:
		if source.IsNil() {
			return reflect.Zero(source.Type())
		}
		result := reflect.New(source.Type().Elem())
		result.Elem().Set(cloneTypedValue(source.Elem()))
		return result
	case reflect.Map:
		if source.IsNil() {
			return reflect.Zero(source.Type())
		}
		result := reflect.MakeMapWithSize(source.Type(), source.Len())
		iterator := source.MapRange()
		for iterator.Next() {
			result.SetMapIndex(cloneTypedValue(iterator.Key()), cloneTypedValue(iterator.Value()))
		}
		return result
	case reflect.Slice:
		if source.IsNil() {
			return reflect.Zero(source.Type())
		}
		result := reflect.MakeSlice(source.Type(), source.Len(), source.Cap())
		for index := range source.Len() {
			result.Index(index).Set(cloneTypedValue(source.Index(index)))
		}
		return result
	case reflect.Array:
		result := reflect.New(source.Type()).Elem()
		for index := range source.Len() {
			result.Index(index).Set(cloneTypedValue(source.Index(index)))
		}
		return result
	case reflect.Struct:
		result := reflect.New(source.Type()).Elem()
		result.Set(source)
		cloneExportedFields(result, source)
		return result
	default:
		return source
	}
}

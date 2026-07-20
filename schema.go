package vltconfig

import (
	"encoding"
	"fmt"
	"reflect"
	"strings"
	"unicode"
)

const (
	jsonTagName     = "json"
	envTagName      = "env"
	validationTag   = "vlt"
	requiredTagName = "required"
	nestedEnvSep    = "__"
)

var textUnmarshalerType = reflect.TypeFor[encoding.TextUnmarshaler]()

type compiledSchema struct {
	root   reflect.Type
	fields []*fieldSchema
	nodes  []*fieldSchema
	leaves []*fieldSchema
}

type fieldSchema struct {
	canonicalName string
	path          []string
	index         []int
	typeOf        reflect.Type
	required      bool
	envName       string
	envDisabled   bool
	structured    bool
	pointer       bool
	children      []*fieldSchema
}

func (f *fieldSchema) pathString() string {
	return strings.Join(f.path, ".")
}

func compileSchema(root reflect.Type) (*compiledSchema, error) {
	if root == nil {
		return nil, &SchemaError{Problem: "root type must be a struct"}
	}
	if root.Kind() != reflect.Struct {
		return nil, &SchemaError{Problem: "root type must be a non-pointer struct"}
	}

	result := &compiledSchema{root: root}
	state := schemaCompiler{
		result:       result,
		canonical:    make(map[string]struct{}),
		environments: make(map[string]string),
		active:       make(map[reflect.Type]bool),
	}
	fields, err := state.compileStruct(root, nil, nil, false)
	if err != nil {
		return nil, err
	}
	result.fields = fields
	return result, nil
}

type schemaCompiler struct {
	result       *compiledSchema
	canonical    map[string]struct{}
	environments map[string]string
	active       map[reflect.Type]bool
}

func (c *schemaCompiler) compileStruct(
	typeOf reflect.Type,
	parentPath []string,
	parentIndex []int,
	envDisabled bool,
) ([]*fieldSchema, error) {
	if c.active[typeOf] {
		return nil, &SchemaError{
			Path:    strings.Join(parentPath, "."),
			Problem: "recursive struct schemas are not supported",
		}
	}
	c.active[typeOf] = true
	defer delete(c.active, typeOf)

	fields := make([]*fieldSchema, 0, typeOf.NumField())
	for position := range typeOf.NumField() {
		field := typeOf.Field(position)
		if field.PkgPath != "" {
			continue
		}

		jsonName, excluded, explicitJSON, err := canonicalFieldName(field)
		if err != nil {
			return nil, err
		}
		if excluded {
			continue
		}
		if field.Anonymous && !explicitJSON {
			return nil, &SchemaError{
				Path:    strings.Join(parentPath, "."),
				Problem: fmt.Sprintf("anonymous field %q requires an explicit json tag", field.Name),
			}
		}

		path := appendCopy(parentPath, jsonName)
		pathText := strings.Join(path, ".")
		if _, exists := c.canonical[pathText]; exists {
			return nil, &SchemaError{Path: pathText, Problem: "duplicate canonical path"}
		}
		c.canonical[pathText] = struct{}{}

		required, err := parseValidationTag(field, pathText)
		if err != nil {
			return nil, err
		}

		fieldType := field.Type
		structuredType, pointer, structured := nestedStructType(fieldType)
		if err := validateMapKey(fieldType, pathText); err != nil {
			return nil, err
		}

		disabled, envName, err := effectiveEnvName(field, path, envDisabled, pathText)
		if err != nil {
			return nil, err
		}
		if envName != "" {
			if previous, exists := c.environments[envName]; exists {
				return nil, &SchemaError{
					Path:    pathText,
					Problem: fmt.Sprintf("environment name %q is already used by %q", envName, previous),
				}
			}
			c.environments[envName] = pathText
		}

		node := &fieldSchema{
			canonicalName: jsonName,
			path:          path,
			index:         appendCopy(parentIndex, position),
			typeOf:        fieldType,
			required:      required,
			envName:       envName,
			envDisabled:   disabled,
			structured:    structured,
			pointer:       pointer,
		}
		c.result.nodes = append(c.result.nodes, node)

		if structured {
			children, err := c.compileStruct(structuredType, path, node.index, disabled)
			if err != nil {
				return nil, err
			}
			node.children = children
		} else {
			c.result.leaves = append(c.result.leaves, node)
		}

		fields = append(fields, node)
	}
	return fields, nil
}

func canonicalFieldName(field reflect.StructField) (name string, excluded bool, explicit bool, err error) {
	raw, explicit := field.Tag.Lookup(jsonTagName)
	name = strings.Split(raw, ",")[0]
	if explicit && name == "-" {
		return "", true, true, nil
	}
	if name == "" {
		name = snakeCase(field.Name)
	}
	if name == "" || strings.Contains(name, ".") {
		return "", false, explicit, &SchemaError{
			Path:    field.Name,
			Problem: "json field name must be non-empty and must not contain a dot",
		}
	}
	return name, false, explicit, nil
}

func parseValidationTag(field reflect.StructField, path string) (bool, error) {
	raw := field.Tag.Get(validationTag)
	if raw == "" {
		return false, nil
	}
	required := false
	for _, option := range strings.Split(raw, ",") {
		switch option {
		case requiredTagName:
			required = true
		case "":
			return false, &SchemaError{Path: path, Problem: "empty vlt tag option"}
		default:
			return false, &SchemaError{
				Path:    path,
				Problem: fmt.Sprintf("unsupported vlt tag option %q", option),
			}
		}
	}
	return required, nil
}

func effectiveEnvName(
	field reflect.StructField,
	path []string,
	parentDisabled bool,
	pathText string,
) (disabled bool, name string, err error) {
	if parentDisabled {
		return true, "", nil
	}
	raw, explicit := field.Tag.Lookup(envTagName)
	if explicit {
		if raw == "-" {
			return true, "", nil
		}
		if raw == "" || strings.ContainsRune(raw, '=') || strings.ContainsRune(raw, '\x00') {
			return false, "", &SchemaError{
				Path:    pathText,
				Problem: "env tag must be a non-empty environment name without '=' or NUL",
			}
		}
		return false, raw, nil
	}

	segments := make([]string, len(path))
	for index, segment := range path {
		segments[index] = envSegment(segment)
		if segments[index] == "" {
			return false, "", &SchemaError{
				Path:    pathText,
				Problem: "canonical path cannot produce an environment name",
			}
		}
	}
	return false, strings.Join(segments, nestedEnvSep), nil
}

func nestedStructType(typeOf reflect.Type) (reflect.Type, bool, bool) {
	pointer := typeOf.Kind() == reflect.Pointer
	candidate := typeOf
	if pointer {
		candidate = typeOf.Elem()
	}
	if candidate.Kind() != reflect.Struct || supportsTextUnmarshal(typeOf) {
		return nil, pointer, false
	}
	return candidate, pointer, true
}

func supportsTextUnmarshal(typeOf reflect.Type) bool {
	if typeOf.Implements(textUnmarshalerType) {
		return true
	}
	if typeOf.Kind() != reflect.Pointer && reflect.PointerTo(typeOf).Implements(textUnmarshalerType) {
		return true
	}
	return false
}

func validateMapKey(typeOf reflect.Type, path string) error {
	candidate := typeOf
	if candidate.Kind() == reflect.Pointer {
		candidate = candidate.Elem()
	}
	if candidate.Kind() == reflect.Map && candidate.Key().Kind() != reflect.String {
		return &SchemaError{Path: path, Problem: "configuration maps must use string keys"}
	}
	return nil
}

func appendCopy[T any](values []T, value T) []T {
	result := make([]T, len(values)+1)
	copy(result, values)
	result[len(values)] = value
	return result
}

func snakeCase(value string) string {
	runes := []rune(value)
	var builder strings.Builder
	for index, current := range runes {
		if unicode.IsUpper(current) {
			if index > 0 {
				previous := runes[index-1]
				nextIsLower := index+1 < len(runes) && unicode.IsLower(runes[index+1])
				if unicode.IsLower(previous) || unicode.IsDigit(previous) || (unicode.IsUpper(previous) && nextIsLower) {
					builder.WriteByte('_')
				}
			}
			builder.WriteRune(unicode.ToLower(current))
			continue
		}
		builder.WriteRune(current)
	}
	return builder.String()
}

func envSegment(value string) string {
	var builder strings.Builder
	lastUnderscore := false
	for _, current := range value {
		if unicode.IsLetter(current) || unicode.IsDigit(current) {
			builder.WriteRune(unicode.ToUpper(current))
			lastUnderscore = false
			continue
		}
		if !lastUnderscore {
			builder.WriteByte('_')
			lastUnderscore = true
		}
	}
	return strings.Trim(builder.String(), "_")
}

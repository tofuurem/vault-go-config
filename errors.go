package vltconfig

import (
	"errors"
	"fmt"
	"strings"
)

var (
	// ErrConfig is the root category for errors produced by this package.
	ErrConfig = errors.New("vltconfig: configuration error")
	// ErrSchema reports an unsupported or ambiguous destination schema.
	ErrSchema = errors.New("vltconfig: schema error")
	// ErrDecode reports that merged source data cannot be decoded safely.
	ErrDecode = errors.New("vltconfig: decode error")
	// ErrRequired reports one or more absent externally required fields.
	ErrRequired = errors.New("vltconfig: required field missing")
	// ErrValidation reports rejection by the destination's Validate method.
	ErrValidation = errors.New("vltconfig: validation error")
	// ErrJSON is the root category for JSON source failures.
	ErrJSON = errors.New("vltconfig: JSON source error")
	// ErrJSONNotFound reports an explicitly configured file that is absent.
	ErrJSONNotFound = errors.New("vltconfig: JSON file not found")
	// ErrJSONPermission reports a JSON file that cannot be read due to permissions.
	ErrJSONPermission = errors.New("vltconfig: JSON permission error")
	// ErrJSONMalformed reports malformed UTF-8 or JSON syntax.
	ErrJSONMalformed = errors.New("vltconfig: malformed JSON")
	// ErrJSONStructure reports a JSON document whose root is not an object.
	ErrJSONStructure = errors.New("vltconfig: invalid JSON structure")
)

// SchemaError identifies a safe path and schema rule that could not be
// represented unambiguously.
type SchemaError struct {
	Path    string
	Problem string
}

func (e *SchemaError) Error() string {
	if e.Path == "" {
		return fmt.Sprintf("configuration schema is invalid: %s", e.Problem)
	}
	return fmt.Sprintf("configuration schema is invalid at %q: %s", e.Path, e.Problem)
}

// Is supports both the specific schema and root configuration categories.
func (e *SchemaError) Is(target error) bool {
	return target == ErrSchema || target == ErrConfig
}

// DecodeError reports a value-free conversion failure.
type DecodeError struct {
	Source    string
	Path      string
	InputName string
	CauseType string
}

func (e *DecodeError) Error() string {
	location := e.Path
	if location == "" {
		location = "configuration"
	}
	if e.InputName != "" {
		return fmt.Sprintf("cannot decode %s field %q from %q", e.Source, location, e.InputName)
	}
	return fmt.Sprintf("cannot decode %s field %q", e.Source, location)
}

// Is supports both the specific decode and root configuration categories.
func (e *DecodeError) Is(target error) bool {
	return target == ErrDecode || target == ErrConfig
}

// RequiredError contains canonical paths only, never source values.
type RequiredError struct {
	Paths []string
}

func (e *RequiredError) Error() string {
	return fmt.Sprintf("required configuration fields are missing: %s", strings.Join(e.Paths, ", "))
}

// Is supports both the specific required and root configuration categories.
func (e *RequiredError) Is(target error) bool {
	return target == ErrRequired || target == ErrConfig
}

// ValidationError safely categorizes an error returned by Validate without
// copying its potentially value-bearing message.
type ValidationError struct {
	CauseType string
	cause     error
}

func (e *ValidationError) Error() string {
	return "configuration validation failed"
}

// Unwrap preserves programmatic causality without including the cause text in
// the outer error message.
func (e *ValidationError) Unwrap() error {
	return e.cause
}

// Is supports both the specific validation and root configuration categories.
func (e *ValidationError) Is(target error) bool {
	return target == ErrValidation || target == ErrConfig
}

// JSONError describes a value-free JSON source failure. Path is the resolved
// local file path and CauseType never contains the upstream error message.
type JSONError struct {
	Path      string
	CauseType string
	kind      error
}

func (e *JSONError) Error() string {
	switch e.kind {
	case ErrJSONNotFound:
		return fmt.Sprintf("JSON configuration file was not found at %q", e.Path)
	case ErrJSONPermission:
		return fmt.Sprintf("JSON configuration file is not readable at %q", e.Path)
	case ErrJSONMalformed:
		return fmt.Sprintf("JSON configuration file is malformed at %q", e.Path)
	case ErrJSONStructure:
		return fmt.Sprintf("JSON configuration must contain an object at %q", e.Path)
	default:
		if e.Path == "" {
			return "JSON configuration is invalid"
		}
		return fmt.Sprintf("JSON configuration file could not be read at %q", e.Path)
	}
}

// Is supports the specific JSON kind, JSON source, and root configuration
// categories.
func (e *JSONError) Is(target error) bool {
	return target == e.kind || target == ErrJSON || target == ErrConfig
}

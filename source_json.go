package vltconfig

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

const JSONPathEnvironment = "PYDANTIC_JSON_PATH"

type jsonSource struct {
	lookup       func(string) (string, bool)
	explicitFile string
}

func (jsonSource) name() string {
	return "JSON"
}

func (source jsonSource) load(ctx context.Context, _ *compiledSchema) (map[string]any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	path, explicit, err := source.resolvePath()
	if err != nil {
		return nil, err
	}

	document, err := os.ReadFile(path)
	if err != nil {
		switch {
		case errors.Is(err, fs.ErrNotExist) && !explicit:
			return map[string]any{}, nil
		case errors.Is(err, fs.ErrNotExist):
			return nil, newJSONError(ErrJSONNotFound, path, err)
		case errors.Is(err, fs.ErrPermission):
			return nil, newJSONError(ErrJSONPermission, path, err)
		default:
			return nil, newJSONError(ErrJSON, path, err)
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !utf8.Valid(document) {
		return nil, &JSONError{Path: path, CauseType: "invalid UTF-8", kind: ErrJSONMalformed}
	}

	decoder := json.NewDecoder(bytes.NewReader(document))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, newJSONError(ErrJSONMalformed, path, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		causeType := "trailing JSON data"
		if err != nil {
			causeType = fmt.Sprintf("%T", err)
		}
		return nil, &JSONError{Path: path, CauseType: causeType, kind: ErrJSONMalformed}
	}
	result, ok := value.(map[string]any)
	if !ok {
		return nil, &JSONError{Path: path, CauseType: "non-object root", kind: ErrJSONStructure}
	}
	return cloneStringMap(result), nil
}

func (source jsonSource) resolvePath() (string, bool, error) {
	if source.explicitFile != "" {
		return filepath.Clean(source.explicitFile), true, nil
	}
	directory, present := source.lookup(JSONPathEnvironment)
	if present {
		if strings.TrimSpace(directory) == "" {
			return "", true, &JSONError{CauseType: "empty directory", kind: ErrJSON}
		}
		return filepath.Join(directory, "config.json"), true, nil
	}
	return filepath.Clean("config.json"), false, nil
}

func newJSONError(kind error, path string, cause error) *JSONError {
	causeType := ""
	if cause != nil {
		causeType = fmt.Sprintf("%T", cause)
	}
	return &JSONError{Path: path, CauseType: causeType, kind: kind}
}

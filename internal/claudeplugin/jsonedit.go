package claudeplugin

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// jsonMember is one key of a JSON object with its raw value.
type jsonMember struct {
	key   string
	value json.RawMessage
}

// jsonObject is a JSON object that keeps its keys in file order, so editing
// one key of a file the user owns (.mcp.json, settings.local.json) leaves the
// rest of it as it was.
type jsonObject struct {
	members []jsonMember
}

// parseJSONObject decodes raw as a JSON object, keeping key order.
func parseJSONObject(raw []byte) (*jsonObject, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	opening, openErr := decoder.Token()

	if openErr != nil {
		return nil, openErr
	}

	if delim, isDelim := opening.(json.Delim); !isDelim || delim != '{' {
		return nil, errors.New("not a JSON object")
	}

	object := &jsonObject{}

	for decoder.More() {
		keyToken, keyErr := decoder.Token()

		if keyErr != nil {
			return nil, keyErr
		}

		key, isString := keyToken.(string)

		if !isString {
			return nil, errors.New("object key is not a string")
		}

		var value json.RawMessage

		if valueErr := decoder.Decode(&value); valueErr != nil {
			return nil, valueErr
		}

		object.set(key, value)
	}

	if _, closeErr := decoder.Token(); closeErr != nil {
		return nil, closeErr
	}

	if _, trailingErr := decoder.Token(); !errors.Is(trailingErr, io.EOF) {
		return nil, errors.New("trailing data after JSON object")
	}

	return object, nil
}

// readJSONObject reads path as a JSON object. A missing file reads as
// (nil, false, nil).
func readJSONObject(path string) (*jsonObject, bool, error) {
	raw, readErr := os.ReadFile(path)

	if errors.Is(readErr, os.ErrNotExist) {
		return nil, false, nil
	}

	if readErr != nil {
		return nil, false, readErr
	}

	object, parseErr := parseJSONObject(raw)

	if parseErr != nil {
		return nil, true, fmt.Errorf("%s: %w", path, parseErr)
	}

	return object, true, nil
}

func (object *jsonObject) get(key string) (json.RawMessage, bool) {
	for _, member := range object.members {
		if member.key == key {
			return member.value, true
		}
	}

	return nil, false
}

// set replaces key's value in place, or appends key when it is new.
func (object *jsonObject) set(key string, value json.RawMessage) {
	for index := range object.members {
		if object.members[index].key == key {
			object.members[index].value = value

			return
		}
	}

	object.members = append(object.members, jsonMember{key: key, value: value})
}

func (object *jsonObject) remove(key string) bool {
	for index, member := range object.members {
		if member.key == key {
			object.members = append(object.members[:index], object.members[index+1:]...)

			return true
		}
	}

	return false
}

func (object *jsonObject) isEmpty() bool {
	return len(object.members) == 0
}

// marshal renders the object with two-space indentation. Member values are
// re-indented to sit under their key, so the result nests as a member value.
func (object *jsonObject) marshal() ([]byte, error) {
	var buffer bytes.Buffer

	if object.isEmpty() {
		return []byte("{}"), nil
	}

	buffer.WriteString("{\n")

	for index, member := range object.members {
		key, keyErr := json.Marshal(member.key)

		if keyErr != nil {
			return nil, keyErr
		}

		var value bytes.Buffer

		if indentErr := json.Indent(&value, member.value, "  ", "  "); indentErr != nil {
			return nil, indentErr
		}

		buffer.WriteString("  ")
		buffer.Write(key)
		buffer.WriteString(": ")
		buffer.Write(value.Bytes())

		if index < len(object.members)-1 {
			buffer.WriteString(",")
		}

		buffer.WriteString("\n")
	}

	buffer.WriteString("}")

	return buffer.Bytes(), nil
}

func writeJSONObject(path string, object *jsonObject) error {
	body, marshalErr := object.marshal()

	if marshalErr != nil {
		return marshalErr
	}

	if mkdirErr := os.MkdirAll(filepath.Dir(path), 0o755); mkdirErr != nil {
		return mkdirErr
	}

	return os.WriteFile(path, append(body, '\n'), 0o644)
}

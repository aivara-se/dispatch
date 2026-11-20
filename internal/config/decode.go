package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strconv"
	"strings"
)

// Error names the file and the key that failed the load.
type Error struct {
	File string
	Key  string
	Msg  string
}

// Error renders the file, the key and the reason on one line.
func (e *Error) Error() string {
	if e.Key == "" {
		return e.File + ": " + e.Msg
	}
	return e.File + ": " + e.Key + ": " + e.Msg
}

func decodeFile(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := decodeInto(data, v); err != nil {
		return decodeError(path, "", err)
	}
	return nil
}

func decodeRaw(path, key string, data []byte, v any) error {
	if err := decodeInto(data, v); err != nil {
		return decodeError(path, key, err)
	}
	return nil
}

// decodeError names the key that failed. An unknown field is reported at its
// own path, so the operator sees the file, the task and the field.
func decodeError(path, key string, err error) error {
	const prefix = "json: unknown field "
	msg := err.Error()
	if strings.HasPrefix(msg, prefix) {
		if field, err := strconv.Unquote(strings.TrimPrefix(msg, prefix)); err == nil {
			if key != "" {
				key += "." + field
			} else {
				key = field
			}
			return &Error{File: path, Key: key, Msg: "unknown field"}
		}
	}
	return &Error{File: path, Key: key, Msg: msg}
}

func decodeInto(data []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return err
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return errors.New("trailing data")
		}
		return err
	}
	return nil
}

func keyError(file, key, msg string) error {
	return &Error{File: file, Key: key, Msg: msg}
}

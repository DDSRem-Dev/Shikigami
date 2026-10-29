// Package strictjson rejects ambiguous duplicate members before decoding a
// bounded manifest or persisted administrative record into its strict schema.
package strictjson

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
)

func Decode(data []byte, out any) error {
	check := json.NewDecoder(bytes.NewReader(data))
	check.UseNumber()
	if err := value(check); err != nil {
		return err
	}
	if _, err := check.Token(); !errors.Is(err, io.EOF) {
		return errors.New("trailing JSON content")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	return decoder.Decode(out)
}
func value(d *json.Decoder) error {
	token, err := d.Token()
	if err != nil {
		return err
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delimiter {
	case '{':
		seen := map[string]bool{}
		for d.More() {
			key, err := d.Token()
			if err != nil {
				return err
			}
			name, ok := key.(string)
			if !ok || seen[name] {
				return errors.New("duplicate or invalid JSON member")
			}
			seen[name] = true
			if err = value(d); err != nil {
				return err
			}
		}
		token, err = d.Token()
		if err != nil {
			return err
		}
		if token != json.Delim('}') {
			return errors.New("invalid JSON object")
		}
	case '[':
		for d.More() {
			if err = value(d); err != nil {
				return err
			}
		}
		token, err = d.Token()
		if err != nil {
			return err
		}
		if token != json.Delim(']') {
			return errors.New("invalid JSON array")
		}
	default:
		return errors.New("unexpected JSON delimiter")
	}
	return nil
}

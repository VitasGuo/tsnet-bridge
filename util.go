package main

import (
	"io"
	"strings"

	"gopkg.in/yaml.v3"
)

// yamlUnmarshal wraps yaml.Unmarshal for config loading.
func yamlUnmarshal(data []byte, v any) error {
	return yaml.Unmarshal(data, v)
}

// yamlMarshal wraps yaml.Marshal for config saving.
func yamlMarshal(v any) ([]byte, error) {
	return yaml.Marshal(v)
}

// stringReader returns an io.Reader for a string.
func stringReader(s string) io.Reader {
	return strings.NewReader(s)
}

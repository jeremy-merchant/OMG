package config

import (
	"errors"
	"io"
)

const maxConfigBytes = 1 << 20

func readBoundedConfig(reader io.Reader) ([]byte, error) {
	contents, err := io.ReadAll(io.LimitReader(reader, maxConfigBytes+1))
	if err != nil {
		return nil, err
	}
	if len(contents) > maxConfigBytes {
		return nil, errors.New("project configuration is too large")
	}
	return contents, nil
}

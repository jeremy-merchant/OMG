package cli

import (
	"errors"
	"io"

	"example.invalid/coordledger/internal/safety"
)

const maxApplicationPayload = 1 << 20

func loadApplicationPayload(request Request, input io.Reader) (string, error) {
	sources := 0
	if request.PayloadProvided {
		sources++
	}
	if request.PayloadFileProvided {
		sources++
	}
	if request.PayloadStdin {
		sources++
	}
	if sources != 1 {
		return "", errors.New("exactly one application payload source is required")
	}

	if request.PayloadProvided {
		if request.Payload == "" || safety.Reject(request.Payload) != nil || len(request.Payload) > maxApplicationPayload {
			return "", errors.New("inline application payload is unsafe")
		}
		return request.Payload, nil
	}

	var (
		payload []byte
		err     error
	)
	if request.PayloadFileProvided {
		if request.PayloadFile == "" {
			return "", errors.New("application payload path is unsafe")
		}
		if safety.Reject(request.PayloadFile) != nil {
			return "", errors.New("application payload path is unsafe")
		}
		payload, err = readPrivatePayloadFile(request.PayloadFile)
	} else {
		payload, err = readBoundedPayload(input)
	}
	if err != nil || len(payload) == 0 {
		return "", errors.New("application payload could not be read safely")
	}
	return string(payload), nil
}

func readBoundedPayload(input io.Reader) ([]byte, error) {
	payload, err := io.ReadAll(io.LimitReader(input, maxApplicationPayload+1))
	if err != nil {
		return nil, err
	}
	if len(payload) > maxApplicationPayload {
		return nil, errors.New("application payload is too large")
	}
	return payload, nil
}

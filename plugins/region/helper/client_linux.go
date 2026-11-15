//go:build linux

package helper

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"time"
)

func (Client) Call(parent context.Context, action string, request Request) (Response, error) {
	allowed := false
	for _, candidate := range Actions {
		if action == candidate {
			allowed = true
		}
	}
	if !allowed {
		return Response{}, errors.New("unsupported region helper action")
	}
	if err := installedHelperTrusted(); err != nil {
		return Response{}, err
	}
	data, err := json.Marshal(request)
	if err != nil || len(data) > MaxRequestBytes {
		return Response{}, errors.New("invalid or oversized region helper request")
	}
	ctx, cancel := context.WithTimeout(parent, 80*time.Second)
	defer cancel()
	return callHelperCommand(ctx, exec.Command("/usr/bin/sudo", "-n", "--", ExecutablePath, action), data)
}

// Command injection is in-process only: Call always selects the fixed sudo and
// installed helper paths above. A caller cannot choose a command or its options.
func callHelperCommand(ctx context.Context, command *exec.Cmd, data []byte) (Response, error) {
	command.Stdin = bytes.NewReader(data)
	stdout := &limitedBuffer{maximum: MaxResponseBytes}
	stderr := &limitedBuffer{maximum: 4096}
	command.Stdout, command.Stderr = stdout, stderr
	completed := make(chan error, 1)
	go func() { completed <- runOwnedCommand(ctx, command) }()
	var runErr error
	select {
	case <-ctx.Done():
		// A privilege-changing sudo process may not be signalable by this UID.
		// Do not retain the client waiting for it or its pipes. The owned waiter
		// still reaps it; the root journal/guard owns any interrupted operation.
		return Response{}, ctx.Err()
	case runErr = <-completed:
	}
	if ctx.Err() != nil {
		return Response{}, ctx.Err()
	}
	var response Response
	decoder := json.NewDecoder(bytes.NewReader(stdout.Bytes()))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&response); err != nil {
		return Response{}, fmt.Errorf("installed region helper unavailable or returned invalid bounded JSON: %w", runErrOr(runErr, err))
	}
	if decoder.Decode(new(any)) != io.EOF {
		return Response{}, errors.New("installed region helper returned trailing JSON")
	}
	if runErr != nil && response.Error == "" {
		return Response{}, errors.New("installed region helper failed without an error response")
	}
	return response, nil
}

func runErrOr(first, second error) error {
	if first != nil {
		return first
	}
	return second
}

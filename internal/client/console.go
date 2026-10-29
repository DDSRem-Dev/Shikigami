package client

import (
	"context"
	"errors"
	"os"

	"golang.org/x/term"
)

func ReadConsoleLine(ctx context.Context, input *os.File, limit int) (string, error) {
	if !term.IsTerminal(int(input.Fd())) {
		return "", errors.New("interactive input requires a terminal")
	}
	file, e := openTerminalFD(input)
	if e != nil {
		return "", e
	}
	defer file.Close()
	var data []byte
	one := make([]byte, 1)
	for {
		n, e := file.Read(ctx, one)
		if e != nil {
			return "", e
		}
		if n == 0 {
			continue
		}
		if one[0] == '\n' || one[0] == '\r' {
			return string(data), nil
		}
		data = append(data, one[0])
		if len(data) > limit {
			return "", errors.New("input is too long")
		}
	}
}
func ReadSecret(ctx context.Context, input *os.File, limit int) (secret []byte, result error) {
	if !term.IsTerminal(int(input.Fd())) {
		return nil, errors.New("credential input requires a terminal or explicit --token-stdin")
	}
	original, e := term.MakeRaw(int(input.Fd()))
	if e != nil {
		return nil, e
	}
	defer func() { result = errors.Join(result, term.Restore(int(input.Fd()), original)) }()
	file, e := openTerminalFD(input)
	if e != nil {
		return nil, e
	}
	defer file.Close()
	one := make([]byte, 1)
	for {
		n, e := file.Read(ctx, one)
		if e != nil {
			return nil, e
		}
		if n == 0 {
			continue
		}
		switch one[0] {
		case 3, 4:
			return nil, context.Canceled
		case '\n', '\r':
			return secret, nil
		case 8, 127:
			if len(secret) > 0 {
				secret = secret[:len(secret)-1]
			}
		default:
			secret = append(secret, one[0])
			if len(secret) > limit {
				return nil, errors.New("credential is too long")
			}
		}
	}
}

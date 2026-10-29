// Package tunnel implements the one-TCP-connection PortData state machine.
package tunnel

import (
	"connectrpc.com/connect/v2"
	"context"
	"errors"
	sdk "github.com/DDSRem-Dev/Shikigami/pkg/plugin"
	limits "github.com/DDSRem-Dev/Shikigami/pkg/plugin/limits"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

type Options struct {
	ID          string
	Idle        time.Duration
	Server      bool
	CloseSend   func() error
	CloseStream func() error
	Count       func(int)
}

var errPeerClosed = errors.New("peer closed tunnel")

func invalid(s string) error { return connect.NewError(connect.CodeInvalidArgument, s) }
func ConnectFrame(p *pb.PortData) error {
	if p == nil || p.Type != pb.PortData_CONNECT || p.ConnectionId == "" || len(p.ConnectionId) > 128 || len(p.Data) != 0 || p.Port < 1 || p.Port > 65535 {
		return invalid("invalid CONNECT frame")
	}
	return nil
}
func ConnectedFrame(p *pb.PortData, id string) error {
	if p == nil || p.Type != pb.PortData_CONNECTED || p.ConnectionId != id || p.Port != 0 || p.RemoteAddress != "" || p.SessionId != "" || p.Automatic || p.SourceIdentity != "" || len(p.Data) != 0 {
		return invalid("invalid CONNECTED frame")
	}
	return nil
}

// Bridge has one sender per stream. EOF only half-closes that direction. A
// server's final CLOSE waits for both directions, preserving delayed responses.
// Server RPC cancellation is owned by its transport: returning on a hard error
// releases any outstanding Receive/Send along with the RPC handler.
func Bridge(parent context.Context, conn *net.TCPConn, stream sdk.PortServerStream, o Options) error {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	defer conn.Close()
	var closeOnce sync.Once
	closeAll := func() {
		closeOnce.Do(func() {
			cancel()
			conn.Close()
			if o.CloseStream != nil {
				_ = o.CloseStream()
			}
		})
	}
	defer closeAll()
	stop := context.AfterFunc(ctx, func() {
		conn.Close()
		if o.CloseStream != nil {
			_ = o.CloseStream()
		}
	})
	defer stop()
	var expired atomic.Bool
	var timer *time.Timer
	if o.Idle > 0 {
		timer = time.AfterFunc(o.Idle, func() { expired.Store(true); cancel() })
		defer timer.Stop()
	}
	touch := func() {
		if timer != nil {
			timer.Reset(o.Idle)
		}
		if o.Idle > 0 {
			_ = conn.SetDeadline(time.Now().Add(o.Idle))
		}
	}
	touch()
	count := func(n int) {
		if o.Count != nil {
			o.Count(n)
		}
	}
	recvDone := make(chan struct{})
	results := make(chan error, 2)
	go func() { err := receive(conn, stream, o, touch, count); close(recvDone); results <- err }()
	go func() { err := send(ctx, conn, stream, o, recvDone, touch, count); results <- err }()
	var first error
	for i := 0; i < 2; i++ {
		select {
		case err := <-results:
			if err != nil {
				first = err
				closeAll()
				if o.CloseStream == nil {
					return normalize(first)
				}
				for j := i + 1; j < 2; j++ {
					<-results
				}
				return normalize(first)
			}
		case <-ctx.Done():
			closeAll()
			if o.CloseStream != nil {
				for j := i; j < 2; j++ {
					<-results
				}
			}
			if expired.Load() {
				return connect.NewError(connect.CodeDeadlineExceeded, "PORT_IDLE_TIMEOUT")
			}
			return ctx.Err()
		}
	}
	return nil
}
func normalize(err error) error {
	var timeout net.Error
	if errors.As(err, &timeout) && timeout.Timeout() {
		return connect.NewError(connect.CodeDeadlineExceeded, "PORT_IDLE_TIMEOUT")
	}
	if errors.Is(err, errPeerClosed) {
		return nil
	}
	return err
}
func receive(conn *net.TCPConn, s sdk.PortServerStream, o Options, touch func(), count func(int)) error {
	half := false
	closed := false
	for {
		p, err := s.Receive()
		if errors.Is(err, io.EOF) {
			if !half || (!o.Server && !closed) {
				return invalid("stream ended before HALF_CLOSE/CLOSE")
			}
			return nil
		}
		if err != nil {
			return err
		}
		if closed {
			return invalid("frame after CLOSE")
		}
		if p == nil || p.ConnectionId != o.ID || p.Port != 0 || p.RemoteAddress != "" || p.SessionId != "" || p.Automatic || p.SourceIdentity != "" {
			return invalid("connection identity or target changed")
		}
		if len(p.Data) > limits.MaxDataBytes {
			return connect.NewError(connect.CodeResourceExhausted, "port data exceeds 256 KiB")
		}
		if p.Type != pb.PortData_DATA && len(p.Data) != 0 {
			return invalid("control frame carries data")
		}
		switch p.Type {
		case pb.PortData_DATA:
			if half {
				return invalid("DATA after HALF_CLOSE")
			}
			remaining := p.Data
			for len(remaining) > 0 {
				n, e := conn.Write(remaining)
				if n > 0 {
					count(n)
					touch()
					remaining = remaining[n:]
				}
				if e != nil {
					return e
				}
				if n == 0 {
					return io.ErrNoProgress
				}
			}
		case pb.PortData_HALF_CLOSE:
			if half {
				return invalid("duplicate HALF_CLOSE")
			}
			half = true
			if err = conn.CloseWrite(); err != nil {
				return err
			}
			touch()
		case pb.PortData_CLOSE:
			if o.Server {
				return errPeerClosed
			}
			if !half {
				return invalid("CLOSE before HALF_CLOSE")
			}
			closed = true
		default:
			return invalid("unexpected port frame")
		}
	}
}
func send(ctx context.Context, conn *net.TCPConn, s sdk.PortServerStream, o Options, recvDone <-chan struct{}, touch func(), count func(int)) error {
	buf := make([]byte, limits.MaxDataBytes)
	for {
		n, err := conn.Read(buf)
		if n > 0 {
			// Transports may retain sent messages for tracing or stats after Send
			// returns. Only the private read buffer may be reused by the next Read.
			data := make([]byte, n)
			copy(data, buf[:n])
			if e := s.Send(&pb.PortData{Type: pb.PortData_DATA, ConnectionId: o.ID, Data: data}); e != nil {
				return e
			}
			count(n)
			touch()
		}
		if err != nil {
			if !errors.Is(err, io.EOF) {
				return err
			}
			if e := s.Send(&pb.PortData{Type: pb.PortData_HALF_CLOSE, ConnectionId: o.ID}); e != nil {
				return e
			}
			if o.CloseSend != nil {
				return o.CloseSend()
			}
			if o.Server {
				select {
				case <-recvDone:
					return s.Send(&pb.PortData{Type: pb.PortData_CLOSE, ConnectionId: o.ID})
				case <-ctx.Done():
					return ctx.Err()
				}
			}
			return nil
		}
	}
}

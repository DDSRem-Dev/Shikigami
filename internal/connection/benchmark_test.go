package connection

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	rpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/protocolconnect"
)

// BenchmarkRPCTransport measures the production HTTP/2/protobuf adapters over
// actual local sockets. It deliberately excludes business auth, tmux, plugin
// routing, disk I/O and WAN latency; it is not a product capacity claim.
func BenchmarkRPCTransport(b *testing.B) {
	for _, kind := range []string{"unix", "tls"} {
		b.Run(kind, func(b *testing.B) {
			endpoint := testEndpoint(b, kind, &transportProbe{})
			client := rpc.NewShikiDaemonClient(NewRPCClient(endpoint))
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			if _, err := client.NegotiateProtocol(ctx, &pb.ProtocolOffer{Major: 1}); err != nil {
				b.Fatal(err)
			}
			b.Run("unary_warm", func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					call, cancel := context.WithTimeout(ctx, 5*time.Second)
					response, err := client.NegotiateProtocol(call, &pb.ProtocolOffer{Major: 1})
					cancel()
					if err != nil || response.GetMajor() != 1 {
						b.Fatalf("unary failed: %v", err)
					}
				}
			})
			b.Run("upload_1MiB_64KiB_chunks", func(b *testing.B) {
				chunk := &pb.WriteFileRequest{Payload: &pb.WriteFileRequest_Chunk{Chunk: make([]byte, 64<<10)}}
				b.SetBytes(1 << 20)
				b.ReportAllocs()
				for b.Loop() {
					call, cancel := context.WithTimeout(ctx, 5*time.Second)
					stream, err := client.WriteFile(call)
					if err != nil {
						cancel()
						b.Fatal(err)
					}
					for range 16 {
						if err := stream.Send(chunk); err != nil {
							cancel()
							b.Fatal(err)
						}
					}
					info, err := stream.CloseAndReceive()
					cancel()
					if err != nil || info.GetSize() != 1<<20 {
						b.Fatalf("upload final status/size failed: %v", err)
					}
				}
			})
			b.Run("bidi_open_response_half_close", func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					call, cancel := context.WithTimeout(ctx, 5*time.Second)
					stream, err := client.TerminalStream(call)
					if err != nil {
						cancel()
						b.Fatal(err)
					}
					err = stream.Send(&pb.TerminalInput{Payload: &pb.TerminalInput_Open{Open: &pb.TerminalOpen{SessionId: "benchmark"}}})
					if err == nil {
						var output *pb.TerminalOutput
						output, err = stream.Receive()
						if err == nil && string(output.GetData()) != "ready" {
							err = errors.New("missing response before half-close")
						}
					}
					if err == nil {
						err = stream.CloseSend()
					}
					if err == nil {
						var output *pb.TerminalOutput
						output, err = stream.Receive()
						if err == nil && string(output.GetData()) != "after EOF" {
							err = errors.New("missing final data after half-close")
						}
					}
					if err == nil {
						_, err = stream.Receive()
						if errors.Is(err, io.EOF) {
							err = nil
						} else if err == nil {
							err = errors.New("missing stream final status")
						}
					}
					_ = stream.Close()
					cancel()
					if err != nil {
						b.Fatal(err)
					}
				}
			})
		})
	}
}

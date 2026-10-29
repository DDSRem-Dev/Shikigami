package plugin

import (
	"context"
	"errors"
	"io"

	"connectrpc.com/connect/v2"
	"github.com/DDSRem-Dev/Shikigami/internal/connection"
	sdk "github.com/DDSRem-Dev/Shikigami/pkg/plugin"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	rpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/protocolconnect"
)

// DaemonDataPlane implements only the file/port portion of ShikiDaemon. The
// daemon composes these methods with its separately authenticated core methods.
// Both instances must be ready server-side workers; no fallback I/O is used.
type DaemonDataPlane struct {
	rpc.UnimplementedShikiDaemonHandler
	FileWorker, PortWorker *Instance
	Supervisor             *Supervisor
}

func (i *Instance) workerCall(ctx context.Context, capability string) (context.Context, func(), error) {
	if i == nil {
		return nil, nil, connect.NewError(connect.CodeFailedPrecondition, "plugin is unavailable")
	}
	i.mu.Lock()
	ready := i.snapshot.State == "ready" && i.snapshot.Side == "server"
	compatible := false
	if i.snapshot.Protocol != nil {
		for _, available := range i.snapshot.Protocol.Capabilities {
			if available == capability {
				compatible = true
				break
			}
		}
	}
	ready = ready && compatible
	i.mu.Unlock()
	if !ready {
		return nil, nil, connect.NewError(connect.CodeFailedPrecondition, "server plugin is not ready")
	}
	call, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(i.grant.lifetime, func() { cancel(); closeInput(ctx); abortOutput(ctx) })
	return call, func() { stop(); cancel() }, nil
}
func (d *DaemonDataPlane) ListFiles(ctx context.Context, r *pb.ListFilesRequest) (*pb.ListFilesResponse, error) {
	worker := d.worker("sync")
	ctx, cancel, e := worker.workerCall(ctx, sdk.FileCapability)
	if e != nil {
		return nil, e
	}
	defer cancel()
	v, e := worker.lifecycle.Files().ListFiles(ctx, r)
	return v, connection.RebuildError(e)
}
func (d *DaemonDataPlane) StatFile(ctx context.Context, r *pb.StatFileRequest) (*pb.FileInfo, error) {
	worker := d.worker("sync")
	ctx, cancel, e := worker.workerCall(ctx, sdk.FileCapability)
	if e != nil {
		return nil, e
	}
	defer cancel()
	v, e := worker.lifecycle.Files().StatFile(ctx, r)
	return v, connection.RebuildError(e)
}
func (d *DaemonDataPlane) DeleteFile(ctx context.Context, r *pb.DeleteFileRequest) (*pb.Empty, error) {
	worker := d.worker("sync")
	ctx, cancel, e := worker.workerCall(ctx, sdk.FileCapability)
	if e != nil {
		return nil, e
	}
	defer cancel()
	if e = worker.lifecycle.Files().DeleteFile(ctx, r); e != nil {
		return nil, connection.RebuildError(e)
	}
	return &pb.Empty{}, nil
}
func (d *DaemonDataPlane) ReadFile(ctx context.Context, r *pb.ReadFileRequest, s rpc.ShikiDaemonReadFileServerStream) error {
	worker := d.worker("sync")
	ctx, cancel, e := worker.workerCall(ctx, sdk.FileCapability)
	if e != nil {
		return e
	}
	defer cancel()
	down, e := worker.lifecycle.Files().ReadFile(ctx, r)
	if e != nil {
		return connection.RebuildError(e)
	}
	defer down.Close()
	return forwardResponses(down, s.Send)
}
func (d *DaemonDataPlane) WatchFiles(ctx context.Context, r *pb.WatchFilesRequest, s rpc.ShikiDaemonWatchFilesServerStream) error {
	worker := d.worker("sync")
	ctx, cancel, e := worker.workerCall(ctx, sdk.FileCapability)
	if e != nil {
		return e
	}
	defer cancel()
	down, e := worker.lifecycle.Files().WatchFiles(ctx, r)
	if e != nil {
		return connection.RebuildError(e)
	}
	defer down.Close()
	return forwardResponses(down, s.Send)
}
func (d *DaemonDataPlane) WriteFile(ctx context.Context, s rpc.ShikiDaemonWriteFileServerStream) (*pb.FileInfo, error) {
	worker := d.worker("sync")
	ctx, cancel, e := worker.workerCall(ctx, sdk.FileCapability)
	if e != nil {
		return nil, e
	}
	defer cancel()
	down, e := worker.lifecycle.Files().WriteFile(ctx)
	if e != nil {
		return nil, connection.RebuildError(e)
	}
	defer down.Close()
	for {
		r, err := s.Receive()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, connection.RebuildError(err)
		}
		if err = down.Send(r); err != nil {
			return nil, connection.FinalSendError(ctx, cancel, err, func() error { _, final := down.CloseAndReceive(); return final })
		}
	}
	result, e := down.CloseAndReceive()
	return result, connection.RebuildError(e)
}
func (d *DaemonDataPlane) ListPorts(ctx context.Context, r *pb.ListPortsRequest) (*pb.ListPortsResponse, error) {
	worker := d.worker("port")
	ctx, cancel, e := worker.workerCall(ctx, sdk.PortCapability)
	if e != nil {
		return nil, e
	}
	defer cancel()
	v, e := worker.lifecycle.Ports().ListPorts(ctx, r)
	return v, connection.RebuildError(e)
}
func (d *DaemonDataPlane) WatchPorts(ctx context.Context, r *pb.WatchPortsRequest, s rpc.ShikiDaemonWatchPortsServerStream) error {
	worker := d.worker("port")
	ctx, cancel, e := worker.workerCall(ctx, sdk.PortCapability)
	if e != nil {
		return e
	}
	defer cancel()
	down, e := worker.lifecycle.Ports().WatchPorts(ctx, r)
	if e != nil {
		return connection.RebuildError(e)
	}
	defer down.Close()
	return forwardResponses(down, s.Send)
}
func (d *DaemonDataPlane) ForwardPort(ctx context.Context, s rpc.ShikiDaemonForwardPortServerStream) error {
	worker := d.worker("port")
	call, cancel, e := worker.workerCall(ctx, sdk.PortCapability)
	if e != nil {
		return e
	}
	defer cancel()
	down, e := worker.lifecycle.Ports().ForwardPort(call)
	if e != nil {
		return connection.RebuildError(e)
	}
	defer down.Close()
	return bridgePort(ctx, cancel, s.Receive, s.Send, down)
}
func forwardResponses[T any](down sdk.Receiver[T], send func(*T) error) error {
	for {
		message, err := down.Receive()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return connection.RebuildError(err)
		}
		if err = send(message); err != nil {
			return err
		}
	}
}

func (d *DaemonDataPlane) worker(id string) *Instance {
	if d.Supervisor != nil {
		return d.Supervisor.current(id)
	}
	if id == "sync" {
		return d.FileWorker
	}
	return d.PortWorker
}

func (d *DaemonDataPlane) CheckPort(ctx context.Context, r *pb.CheckPortRequest) (*pb.CheckPortResponse, error) {
	worker := d.worker("port")
	ctx, cancel, e := worker.workerCall(ctx, sdk.PortCheckCapability)
	if e != nil {
		return nil, e
	}
	defer cancel()
	v, e := worker.lifecycle.Ports().CheckPort(ctx, r)
	return v, connection.RebuildError(e)
}

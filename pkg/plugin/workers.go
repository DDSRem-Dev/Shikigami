package plugin

import (
	"context"

	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	ppb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin"
)

// Services are explicit server-side data planes. Client plugins cannot register
// these services; their local worker APIs are separately versioned.
type Services struct {
	Files FileWorkerService
	Ports PortService
	Sync  SyncWorker
	Port  PortWorker
}

const FileCapability = "sync.revision.v1"
const PortCapability = "port.tcp-half-close.v1"
const SyncCapability = "sync.worker.v1"

type SyncWorker interface {
	ConfigureMapping(context.Context, *ppb.Mapping) (*ppb.SyncState, error)
	GetSyncState(context.Context, *ppb.Scope) (*ppb.SyncState, error)
	ListConflicts(context.Context, *ppb.Scope) (*ppb.ConflictList, error)
	ResolveConflict(context.Context, *ppb.ResolveConflictRequest) (*ppb.SyncState, error)
	ReadConflictFile(context.Context, *ppb.ReadConflictFileRequest, func(*pb.FileData) error) error
}
type SyncClient interface {
	ConfigureMapping(context.Context, *ppb.Mapping) (*ppb.SyncState, error)
	GetSyncState(context.Context, *ppb.Scope) (*ppb.SyncState, error)
	ListConflicts(context.Context, *ppb.Scope) (*ppb.ConflictList, error)
	ResolveConflict(context.Context, *ppb.ResolveConflictRequest) (*ppb.SyncState, error)
	ReadConflictFile(context.Context, *ppb.ReadConflictFileRequest) (Receiver[pb.FileData], error)
}

type fileOperations interface {
	ListFiles(context.Context, *pb.ListFilesRequest) (*pb.ListFilesResponse, error)
	StatFile(context.Context, *pb.StatFileRequest) (*pb.FileInfo, error)
	ReadFile(context.Context, *pb.ReadFileRequest, func(*pb.FileData) error) error
	DeleteFile(context.Context, *pb.DeleteFileRequest) error
	WatchFiles(context.Context, *pb.WatchFilesRequest, func(*pb.FileEvent) error) error
}

// FileService is the synchronous file interface used by local reconciliation.
type FileService interface {
	fileOperations
	WriteFile(context.Context, func() (*pb.WriteFileRequest, error)) (*pb.FileInfo, error)
}

// FileWorkerService is registered on the SDK's server transport. Its receiver
// observes the supplied operation context. A canceled receive is terminal;
// implementations must return rather than retry that stream.
type FileWorkerService interface {
	fileOperations
	WriteFile(context.Context, func(context.Context) (*pb.WriteFileRequest, error)) (*pb.FileInfo, error)
}
type PortService interface {
	CheckPort(context.Context, *pb.CheckPortRequest) (*pb.CheckPortResponse, error)
	ListPorts(context.Context, *pb.ListPortsRequest) (*pb.ListPortsResponse, error)
	WatchPorts(context.Context, *pb.WatchPortsRequest, func(*pb.PortEvent) error) error
	ForwardPort(context.Context, PortServerStream) error
}
type PortServerStream interface {
	Send(*pb.PortData) error
	Receive() (*pb.PortData, error)
}

type Receiver[T any] interface {
	Receive() (*T, error)
	Close() error
}
type FileWriteStream interface {
	Send(*pb.WriteFileRequest) error
	CloseAndReceive() (*pb.FileInfo, error)
	Close() error
}
type PortClientStream interface {
	PortServerStream
	CloseSend() error
	Close() error
}
type FileClient interface {
	ListFiles(context.Context, *pb.ListFilesRequest) (*pb.ListFilesResponse, error)
	StatFile(context.Context, *pb.StatFileRequest) (*pb.FileInfo, error)
	ReadFile(context.Context, *pb.ReadFileRequest) (Receiver[pb.FileData], error)
	WriteFile(context.Context) (FileWriteStream, error)
	DeleteFile(context.Context, *pb.DeleteFileRequest) error
	WatchFiles(context.Context, *pb.WatchFilesRequest) (Receiver[pb.FileEvent], error)
}
type PortClient interface {
	CheckPort(context.Context, *pb.CheckPortRequest) (*pb.CheckPortResponse, error)
	ListPorts(context.Context, *pb.ListPortsRequest) (*pb.ListPortsResponse, error)
	WatchPorts(context.Context, *pb.WatchPortsRequest) (Receiver[pb.PortEvent], error)
	ForwardPort(context.Context) (PortClientStream, error)
}

// PortWorker owns local listeners; the host persists their desired configuration.
type PortWorker interface {
	DiscoverForwards(context.Context, *ppb.Scope) (*ppb.ForwardCandidates, error)
	StartForward(context.Context, *ppb.ForwardSpec) (*ppb.ForwardState, error)
	StopForward(context.Context, *ppb.ForwardID) error
	ListForwards(context.Context) (*ppb.ForwardList, error)
}
type PortWorkerClient interface {
	DiscoverForwards(context.Context, *ppb.Scope) (*ppb.ForwardCandidates, error)
	StartForward(context.Context, *ppb.ForwardSpec) (*ppb.ForwardState, error)
	StopForward(context.Context, *ppb.ForwardID) error
	ListForwards(context.Context) (*ppb.ForwardList, error)
}

const PortWorkerCapability = "port.worker.v1"

const PortCheckCapability = "port.authorization.v1"
const PortAutoCapability = "port.auto-forward.v1"

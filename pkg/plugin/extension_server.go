package plugin

import (
	"context"
	"errors"
	"io"
	"sync/atomic"

	"connectrpc.com/connect/v2"
	"github.com/DDSRem-Dev/Shikigami/pkg/plugin/limits"
	epb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/extensions"
	"google.golang.org/grpc"
)

type regionServer struct {
	epb.UnimplementedRegionServiceServer
	impl RegionService
}

func (s *regionServer) InspectRegion(ctx context.Context, req *epb.RegionInspectRequest) (*epb.RegionStatus, error) {
	result, err := s.impl.InspectRegion(ctx, req)
	return result, ToGRPC(err)
}
func (s *regionServer) PlanRegion(ctx context.Context, req *epb.RegionPlanRequest) (*epb.RegionPlan, error) {
	result, err := s.impl.PlanRegion(ctx, req)
	return result, ToGRPC(err)
}
func (s *regionServer) ApplyRegion(ctx context.Context, req *epb.RegionApplyRequest) (*epb.RegionOperation, error) {
	result, err := s.impl.ApplyRegion(ctx, req)
	return result, ToGRPC(err)
}
func (s *regionServer) ConfirmRegion(ctx context.Context, req *epb.RegionOperationRequest) (*epb.RegionOperation, error) {
	result, err := s.impl.ConfirmRegion(ctx, req)
	return result, ToGRPC(err)
}
func (s *regionServer) RollbackRegion(ctx context.Context, req *epb.RegionOperationRequest) (*epb.RegionOperation, error) {
	result, err := s.impl.RollbackRegion(ctx, req)
	return result, ToGRPC(err)
}
func (s *regionServer) GetRegionOperation(ctx context.Context, req *epb.RegionOperationRequest) (*epb.RegionOperation, error) {
	result, err := s.impl.GetRegionOperation(ctx, req)
	return result, ToGRPC(err)
}
func (s *regionServer) DiagnoseRegion(ctx context.Context, req *epb.DiagnosticRequest) (*epb.DiagnosticReport, error) {
	result, err := s.impl.DiagnoseRegion(ctx, req)
	return result, ToGRPC(err)
}

type browserServer struct {
	epb.UnimplementedBrowserServiceServer
	impl BrowserService
}

func (s *browserServer) ListBrowserProfiles(ctx context.Context, req *epb.BrowserScope) (*epb.BrowserProfileList, error) {
	result, err := s.impl.ListBrowserProfiles(ctx, req)
	return result, ToGRPC(err)
}
func (s *browserServer) PutBrowserProfile(ctx context.Context, req *epb.PutBrowserProfileRequest) (*epb.BrowserProfile, error) {
	result, err := s.impl.PutBrowserProfile(ctx, req)
	return result, ToGRPC(err)
}
func (s *browserServer) DeleteBrowserProfile(ctx context.Context, req *epb.DeleteBrowserProfileRequest) (*epb.BrowserProfile, error) {
	result, err := s.impl.DeleteBrowserProfile(ctx, req)
	return result, ToGRPC(err)
}
func (s *browserServer) StartBrowser(ctx context.Context, req *epb.BrowserActionRequest) (*epb.BrowserStatus, error) {
	result, err := s.impl.StartBrowser(ctx, req)
	return result, ToGRPC(err)
}
func (s *browserServer) StopBrowser(ctx context.Context, req *epb.BrowserActionRequest) (*epb.BrowserStatus, error) {
	result, err := s.impl.StopBrowser(ctx, req)
	return result, ToGRPC(err)
}
func (s *browserServer) GetBrowserStatus(ctx context.Context, req *epb.BrowserScope) (*epb.BrowserStatus, error) {
	result, err := s.impl.GetBrowserStatus(ctx, req)
	return result, ToGRPC(err)
}
func (s *browserServer) TakeBrowserControl(ctx context.Context, req *epb.BrowserControlRequest) (*epb.BrowserStatus, error) {
	result, err := s.impl.TakeBrowserControl(ctx, req)
	return result, ToGRPC(err)
}
func (s *browserServer) ListBrowserFiles(ctx context.Context, req *epb.BrowserFileRequest) (*epb.BrowserFileList, error) {
	result, err := s.impl.ListBrowserFiles(ctx, req)
	return result, ToGRPC(err)
}
func (s *browserServer) DiagnoseBrowser(ctx context.Context, req *epb.DiagnosticRequest) (*epb.DiagnosticReport, error) {
	result, err := s.impl.DiagnoseBrowser(ctx, req)
	return result, ToGRPC(err)
}
func (s *browserServer) BrowserStream(stream grpc.BidiStreamingServer[epb.BrowserFrame, epb.BrowserFrame]) error {
	return ToGRPC(s.impl.BrowserStream(stream.Context(), &serverBrowserStream{stream: stream}))
}
func (s *browserServer) ReadBrowserFile(req *epb.BrowserFileRequest, stream grpc.ServerStreamingServer[epb.BrowserFileChunk]) error {
	return ToGRPC(s.impl.ReadBrowserFile(stream.Context(), req, func(chunk *epb.BrowserFileChunk) error {
		if err := validateBrowserFileChunk(chunk); err != nil {
			return err
		}
		return FromGRPC(stream.Send(chunk))
	}))
}
func (s *browserServer) WriteBrowserFile(stream grpc.ClientStreamingServer[epb.BrowserFileChunk, epb.BrowserFile]) error {
	var received atomic.Bool
	reader := newMessageReceiver(func() (*epb.BrowserFileChunk, error) {
		chunk, err := stream.Recv()
		if err != nil {
			return nil, FromGRPC(err)
		}
		if err := validateBrowserFileChunk(chunk); err != nil {
			return nil, err
		}
		received.Store(true)
		return chunk, nil
	})
	defer reader.close()
	result, err := s.impl.WriteBrowserFile(stream.Context(), reader.receive)
	if err != nil {
		return ToGRPC(err)
	}
	if err := reader.failure(); err != nil {
		return ToGRPC(err)
	}
	if !received.Load() {
		return ToGRPC(connect.NewError(connect.CodeInvalidArgument, "browser upload required"))
	}
	// Success requires consuming the entire upload and its terminal status.
	if _, err := reader.receive(stream.Context()); !errors.Is(err, io.EOF) {
		if err == nil {
			err = connect.NewError(connect.CodeFailedPrecondition, "worker did not consume browser upload")
		}
		return ToGRPC(err)
	}
	return stream.SendAndClose(result)
}

type serverBrowserStream struct {
	stream grpc.BidiStreamingServer[epb.BrowserFrame, epb.BrowserFrame]
}

func (s *serverBrowserStream) Send(frame *epb.BrowserFrame) error {
	if err := validateBrowserFrame(frame); err != nil {
		return err
	}
	return FromGRPC(s.stream.Send(frame))
}
func (s *serverBrowserStream) Receive() (*epb.BrowserFrame, error) {
	frame, err := s.stream.Recv()
	if err != nil {
		return nil, FromGRPC(err)
	}
	if err := validateBrowserFrame(frame); err != nil {
		return nil, err
	}
	return frame, nil
}

type browserLocalServer struct {
	epb.UnimplementedBrowserLocalServiceServer
	impl BrowserLocalService
}

func (s *browserLocalServer) OpenBrowserViewer(ctx context.Context, req *epb.OpenBrowserViewerRequest) (*epb.BrowserViewer, error) {
	result, err := s.impl.OpenBrowserViewer(ctx, req)
	return result, ToGRPC(err)
}
func (s *browserLocalServer) CloseBrowserViewer(ctx context.Context, req *epb.CloseBrowserViewerRequest) (*epb.BrowserViewer, error) {
	result, err := s.impl.CloseBrowserViewer(ctx, req)
	return result, ToGRPC(err)
}

func validateBrowserFrame(frame *epb.BrowserFrame) error {
	if frame == nil {
		return connect.NewError(connect.CodeInvalidArgument, "browser frame required")
	}
	if len(frame.Data) > limits.MaxDataBytes {
		return connect.NewError(connect.CodeResourceExhausted, "browser frame too large")
	}
	return nil
}

func validateBrowserFileChunk(chunk *epb.BrowserFileChunk) error {
	if chunk == nil {
		return connect.NewError(connect.CodeInvalidArgument, "browser file chunk required")
	}
	if len(chunk.Data) > limits.MaxDataBytes {
		return connect.NewError(connect.CodeResourceExhausted, "browser file chunk too large")
	}
	return nil
}

var _ epb.RegionServiceServer = (*regionServer)(nil)
var _ epb.BrowserServiceServer = (*browserServer)(nil)
var _ epb.BrowserLocalServiceServer = (*browserLocalServer)(nil)

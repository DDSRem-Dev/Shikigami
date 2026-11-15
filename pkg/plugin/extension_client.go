package plugin

import (
	"context"

	epb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/extensions"
	"google.golang.org/grpc"
)

type regionClient struct {
	owner *lifecycleClient
	stub  epb.RegionServiceClient
}

func (c *regionClient) InspectRegion(ctx context.Context, req *epb.RegionInspectRequest) (*epb.RegionStatus, error) {
	result, err := c.stub.InspectRegion(c.owner.context(ctx), req)
	return result, FromGRPC(err)
}
func (c *regionClient) PlanRegion(ctx context.Context, req *epb.RegionPlanRequest) (*epb.RegionPlan, error) {
	result, err := c.stub.PlanRegion(c.owner.context(ctx), req)
	return result, FromGRPC(err)
}
func (c *regionClient) ApplyRegion(ctx context.Context, req *epb.RegionApplyRequest) (*epb.RegionOperation, error) {
	result, err := c.stub.ApplyRegion(c.owner.context(ctx), req)
	return result, FromGRPC(err)
}
func (c *regionClient) ConfirmRegion(ctx context.Context, req *epb.RegionOperationRequest) (*epb.RegionOperation, error) {
	result, err := c.stub.ConfirmRegion(c.owner.context(ctx), req)
	return result, FromGRPC(err)
}
func (c *regionClient) RollbackRegion(ctx context.Context, req *epb.RegionOperationRequest) (*epb.RegionOperation, error) {
	result, err := c.stub.RollbackRegion(c.owner.context(ctx), req)
	return result, FromGRPC(err)
}
func (c *regionClient) GetRegionOperation(ctx context.Context, req *epb.RegionOperationRequest) (*epb.RegionOperation, error) {
	result, err := c.stub.GetRegionOperation(c.owner.context(ctx), req)
	return result, FromGRPC(err)
}
func (c *regionClient) DiagnoseRegion(ctx context.Context, req *epb.DiagnosticRequest) (*epb.DiagnosticReport, error) {
	result, err := c.stub.DiagnoseRegion(c.owner.context(ctx), req)
	return result, FromGRPC(err)
}

type browserClient struct {
	owner *lifecycleClient
	stub  epb.BrowserServiceClient
}

func (c *browserClient) ListBrowserProfiles(ctx context.Context, req *epb.BrowserScope) (*epb.BrowserProfileList, error) {
	result, err := c.stub.ListBrowserProfiles(c.owner.context(ctx), req)
	return result, FromGRPC(err)
}
func (c *browserClient) PutBrowserProfile(ctx context.Context, req *epb.PutBrowserProfileRequest) (*epb.BrowserProfile, error) {
	result, err := c.stub.PutBrowserProfile(c.owner.context(ctx), req)
	return result, FromGRPC(err)
}
func (c *browserClient) DeleteBrowserProfile(ctx context.Context, req *epb.DeleteBrowserProfileRequest) (*epb.BrowserProfile, error) {
	result, err := c.stub.DeleteBrowserProfile(c.owner.context(ctx), req)
	return result, FromGRPC(err)
}
func (c *browserClient) StartBrowser(ctx context.Context, req *epb.BrowserActionRequest) (*epb.BrowserStatus, error) {
	result, err := c.stub.StartBrowser(c.owner.context(ctx), req)
	return result, FromGRPC(err)
}
func (c *browserClient) StopBrowser(ctx context.Context, req *epb.BrowserActionRequest) (*epb.BrowserStatus, error) {
	result, err := c.stub.StopBrowser(c.owner.context(ctx), req)
	return result, FromGRPC(err)
}
func (c *browserClient) GetBrowserStatus(ctx context.Context, req *epb.BrowserScope) (*epb.BrowserStatus, error) {
	result, err := c.stub.GetBrowserStatus(c.owner.context(ctx), req)
	return result, FromGRPC(err)
}
func (c *browserClient) TakeBrowserControl(ctx context.Context, req *epb.BrowserControlRequest) (*epb.BrowserStatus, error) {
	result, err := c.stub.TakeBrowserControl(c.owner.context(ctx), req)
	return result, FromGRPC(err)
}
func (c *browserClient) ListBrowserFiles(ctx context.Context, req *epb.BrowserFileRequest) (*epb.BrowserFileList, error) {
	result, err := c.stub.ListBrowserFiles(c.owner.context(ctx), req)
	return result, FromGRPC(err)
}
func (c *browserClient) DiagnoseBrowser(ctx context.Context, req *epb.DiagnosticRequest) (*epb.DiagnosticReport, error) {
	result, err := c.stub.DiagnoseBrowser(c.owner.context(ctx), req)
	return result, FromGRPC(err)
}
func (c *browserClient) BrowserStream(ctx context.Context) (BrowserClientStream, error) {
	ctx, cancel := context.WithCancel(c.owner.context(ctx))
	stream, err := c.stub.BrowserStream(ctx)
	if err != nil {
		cancel()
		return nil, FromGRPC(err)
	}
	return &clientBrowserStream{stream: stream, cancel: cancel}, nil
}
func (c *browserClient) ReadBrowserFile(ctx context.Context, req *epb.BrowserFileRequest) (Receiver[epb.BrowserFileChunk], error) {
	ctx, cancel := context.WithCancel(c.owner.context(ctx))
	stream, err := c.stub.ReadBrowserFile(ctx, req)
	if err != nil {
		cancel()
		return nil, FromGRPC(err)
	}
	return &browserDownload{receive: func() (*epb.BrowserFileChunk, error) {
		chunk, err := stream.Recv()
		return chunk, FromGRPC(err)
	}, cancel: cancel}, nil
}
func (c *browserClient) WriteBrowserFile(ctx context.Context) (BrowserWriteStream, error) {
	ctx, cancel := context.WithCancel(c.owner.context(ctx))
	stream, err := c.stub.WriteBrowserFile(ctx)
	if err != nil {
		cancel()
		return nil, FromGRPC(err)
	}
	return &browserWriter{stream: stream, cancel: cancel}, nil
}

type clientBrowserStream struct {
	stream grpc.BidiStreamingClient[epb.BrowserFrame, epb.BrowserFrame]
	cancel context.CancelFunc
}

func (s *clientBrowserStream) Send(frame *epb.BrowserFrame) error {
	if err := validateBrowserFrame(frame); err != nil {
		return err
	}
	return FromGRPC(s.stream.Send(frame))
}
func (s *clientBrowserStream) Receive() (*epb.BrowserFrame, error) {
	frame, err := s.stream.Recv()
	if err != nil {
		s.cancel()
		return nil, FromGRPC(err)
	}
	if err := validateBrowserFrame(frame); err != nil {
		s.cancel()
		return nil, err
	}
	return frame, nil
}
func (s *clientBrowserStream) CloseSend() error { return FromGRPC(s.stream.CloseSend()) }
func (s *clientBrowserStream) Close() error     { s.cancel(); return nil }

type browserWriter struct {
	stream grpc.ClientStreamingClient[epb.BrowserFileChunk, epb.BrowserFile]
	cancel context.CancelFunc
}

func (s *browserWriter) Send(chunk *epb.BrowserFileChunk) error {
	if err := validateBrowserFileChunk(chunk); err != nil {
		return err
	}
	return FromGRPC(s.stream.Send(chunk))
}
func (s *browserWriter) CloseSend() error { return FromGRPC(s.stream.CloseSend()) }
func (s *browserWriter) Receive() (*epb.BrowserFile, error) {
	defer s.cancel()
	result := new(epb.BrowserFile)
	if err := s.stream.RecvMsg(result); err != nil {
		return nil, FromGRPC(err)
	}
	return result, nil
}
func (s *browserWriter) CloseAndReceive() (*epb.BrowserFile, error) {
	if err := s.CloseSend(); err != nil {
		defer s.cancel()
		return nil, err
	}
	return s.Receive()
}
func (s *browserWriter) Close() error { s.cancel(); return nil }

type browserLocalClient struct {
	owner *lifecycleClient
	stub  epb.BrowserLocalServiceClient
}

func (c *browserLocalClient) OpenBrowserViewer(ctx context.Context, req *epb.OpenBrowserViewerRequest) (*epb.BrowserViewer, error) {
	result, err := c.stub.OpenBrowserViewer(c.owner.context(ctx), req)
	return result, FromGRPC(err)
}
func (c *browserLocalClient) CloseBrowserViewer(ctx context.Context, req *epb.CloseBrowserViewerRequest) (*epb.BrowserViewer, error) {
	result, err := c.stub.CloseBrowserViewer(c.owner.context(ctx), req)
	return result, FromGRPC(err)
}

// browserDownload validates data after transport errors have been translated.
type browserDownload struct {
	receive func() (*epb.BrowserFileChunk, error)
	cancel  context.CancelFunc
}

func (s *browserDownload) Receive() (*epb.BrowserFileChunk, error) {
	chunk, err := s.receive()
	if err == nil {
		err = validateBrowserFileChunk(chunk)
	}
	if err != nil {
		s.cancel()
		return nil, err
	}
	return chunk, nil
}
func (s *browserDownload) Close() error { s.cancel(); return nil }

var _ RegionClient = (*regionClient)(nil)
var _ BrowserClient = (*browserClient)(nil)
var _ BrowserLocalClient = (*browserLocalClient)(nil)

package plugin

import (
	"context"

	"connectrpc.com/connect/v2"
	epb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/extensions"
	erpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/extensions/extensionsconnect"
)

type coreRegionClient struct {
	stub         erpc.RegionServiceClient
	localInspect bool
}

func requireExtensionServer(id string) error {
	if id == "" {
		return connect.NewError(connect.CodeInvalidArgument, "explicit server_id required")
	}
	return nil
}

func (c *coreRegionClient) InspectRegion(ctx context.Context, req *epb.RegionInspectRequest) (*epb.RegionStatus, error) {
	if req == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, "region request required")
	}
	if !c.localInspect || req.ServerId != "" {
		if err := requireExtensionServer(req.ServerId); err != nil {
			return nil, err
		}
	}
	return c.stub.InspectRegion(ctx, req)
}
func (c *coreRegionClient) PlanRegion(ctx context.Context, req *epb.RegionPlanRequest) (*epb.RegionPlan, error) {
	if err := requireExtensionServer(req.GetServerId()); err != nil {
		return nil, err
	}
	return c.stub.PlanRegion(ctx, req)
}
func (c *coreRegionClient) ApplyRegion(ctx context.Context, req *epb.RegionApplyRequest) (*epb.RegionOperation, error) {
	if err := requireExtensionServer(req.GetServerId()); err != nil {
		return nil, err
	}
	return c.stub.ApplyRegion(ctx, req)
}
func (c *coreRegionClient) ConfirmRegion(ctx context.Context, req *epb.RegionOperationRequest) (*epb.RegionOperation, error) {
	if err := requireExtensionServer(req.GetServerId()); err != nil {
		return nil, err
	}
	return c.stub.ConfirmRegion(ctx, req)
}
func (c *coreRegionClient) RollbackRegion(ctx context.Context, req *epb.RegionOperationRequest) (*epb.RegionOperation, error) {
	if err := requireExtensionServer(req.GetServerId()); err != nil {
		return nil, err
	}
	return c.stub.RollbackRegion(ctx, req)
}
func (c *coreRegionClient) GetRegionOperation(ctx context.Context, req *epb.RegionOperationRequest) (*epb.RegionOperation, error) {
	if err := requireExtensionServer(req.GetServerId()); err != nil {
		return nil, err
	}
	return c.stub.GetRegionOperation(ctx, req)
}
func (c *coreRegionClient) DiagnoseRegion(ctx context.Context, req *epb.DiagnosticRequest) (*epb.DiagnosticReport, error) {
	if err := requireExtensionServer(req.GetServerId()); err != nil {
		return nil, err
	}
	return c.stub.DiagnoseRegion(ctx, req)
}

type coreBrowserClient struct {
	stub   erpc.BrowserServiceClient
	client *connect.Client
}

func (c *coreBrowserClient) ListBrowserProfiles(ctx context.Context, req *epb.BrowserScope) (*epb.BrowserProfileList, error) {
	if err := requireExtensionServer(req.GetServerId()); err != nil {
		return nil, err
	}
	return c.stub.ListBrowserProfiles(ctx, req)
}
func (c *coreBrowserClient) PutBrowserProfile(ctx context.Context, req *epb.PutBrowserProfileRequest) (*epb.BrowserProfile, error) {
	if err := requireExtensionServer(req.GetServerId()); err != nil {
		return nil, err
	}
	return c.stub.PutBrowserProfile(ctx, req)
}
func (c *coreBrowserClient) DeleteBrowserProfile(ctx context.Context, req *epb.DeleteBrowserProfileRequest) (*epb.BrowserProfile, error) {
	if err := requireExtensionServer(req.GetServerId()); err != nil {
		return nil, err
	}
	return c.stub.DeleteBrowserProfile(ctx, req)
}
func (c *coreBrowserClient) StartBrowser(ctx context.Context, req *epb.BrowserActionRequest) (*epb.BrowserStatus, error) {
	if err := requireExtensionServer(req.GetServerId()); err != nil {
		return nil, err
	}
	return c.stub.StartBrowser(ctx, req)
}
func (c *coreBrowserClient) StopBrowser(ctx context.Context, req *epb.BrowserActionRequest) (*epb.BrowserStatus, error) {
	if err := requireExtensionServer(req.GetServerId()); err != nil {
		return nil, err
	}
	return c.stub.StopBrowser(ctx, req)
}
func (c *coreBrowserClient) GetBrowserStatus(ctx context.Context, req *epb.BrowserScope) (*epb.BrowserStatus, error) {
	if err := requireExtensionServer(req.GetServerId()); err != nil {
		return nil, err
	}
	return c.stub.GetBrowserStatus(ctx, req)
}
func (c *coreBrowserClient) TakeBrowserControl(ctx context.Context, req *epb.BrowserControlRequest) (*epb.BrowserStatus, error) {
	if err := requireExtensionServer(req.GetServerId()); err != nil {
		return nil, err
	}
	return c.stub.TakeBrowserControl(ctx, req)
}
func (c *coreBrowserClient) ListBrowserFiles(ctx context.Context, req *epb.BrowserFileRequest) (*epb.BrowserFileList, error) {
	if err := requireExtensionServer(req.GetServerId()); err != nil {
		return nil, err
	}
	return c.stub.ListBrowserFiles(ctx, req)
}
func (c *coreBrowserClient) DiagnoseBrowser(ctx context.Context, req *epb.DiagnosticRequest) (*epb.DiagnosticReport, error) {
	if err := requireExtensionServer(req.GetServerId()); err != nil {
		return nil, err
	}
	return c.stub.DiagnoseBrowser(ctx, req)
}
func (c *coreBrowserClient) BrowserStream(ctx context.Context) (BrowserClientStream, error) {
	ctx, cancel := context.WithCancel(ctx)
	stream, err := c.stub.BrowserStream(ctx)
	if err != nil {
		cancel()
		return nil, err
	}
	return &coreBrowserStream{stream: stream, cancel: cancel}, nil
}
func (c *coreBrowserClient) ReadBrowserFile(ctx context.Context, req *epb.BrowserFileRequest) (Receiver[epb.BrowserFileChunk], error) {
	if err := requireExtensionServer(req.GetServerId()); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(ctx)
	stream, err := c.stub.ReadBrowserFile(ctx, req)
	if err != nil {
		cancel()
		return nil, err
	}
	return &browserDownload{receive: stream.Receive, cancel: cancel}, nil
}
func (c *coreBrowserClient) WriteBrowserFile(ctx context.Context) (BrowserWriteStream, error) {
	ctx, cancel := context.WithCancel(ctx)
	// The generated client-stream wrapper only exposes CloseAndReceive. Use
	// the same typed method schema so a proxy can observe an early final status
	// while its request sender is idle, without half-closing that request.
	stream, err := c.client.CallClientStream(ctx, connect.Spec{
		StreamType: connect.StreamTypeClient,
		Schema:     epb.File_extensions_proto.Services().ByName("BrowserService").Methods().ByName("WriteBrowserFile"),
		Procedure:  erpc.BrowserServiceWriteBrowserFileProcedure,
	})
	if err != nil {
		cancel()
		return nil, err
	}
	return &coreBrowserWriter{stream: stream, cancel: cancel}, nil
}

type coreBrowserStream struct {
	stream          erpc.BrowserServiceBrowserStreamClientStream
	cancel          context.CancelFunc
	server, profile string
	opened          bool
}

func (s *coreBrowserStream) Send(frame *epb.BrowserFrame) error {
	if err := validateBrowserFrame(frame); err != nil {
		return err
	}
	if !s.opened {
		if frame.Kind != epb.BrowserFrame_OPEN {
			return connect.NewError(connect.CodeInvalidArgument, "browser OPEN required")
		}
		if err := requireExtensionServer(frame.ServerId); err != nil {
			return err
		}
		s.server, s.profile, s.opened = frame.ServerId, frame.ProfileId, true
	} else if frame.Kind == epb.BrowserFrame_OPEN || frame.ServerId != "" && frame.ServerId != s.server || frame.ProfileId != "" && frame.ProfileId != s.profile {
		return connect.NewError(connect.CodeInvalidArgument, "browser stream scope changed")
	}
	return s.stream.Send(frame)
}
func (s *coreBrowserStream) Receive() (*epb.BrowserFrame, error) {
	frame, err := s.stream.Receive()
	if err == nil {
		err = validateBrowserFrame(frame)
	}
	if err != nil {
		s.cancel()
	}
	return frame, err
}
func (s *coreBrowserStream) CloseSend() error { return s.stream.CloseSend() }
func (s *coreBrowserStream) Close() error {
	defer s.cancel()
	return s.stream.Close()
}

type coreBrowserWriter struct {
	stream          connect.ClientStream
	cancel          context.CancelFunc
	server, profile string
	opened          bool
}

func (s *coreBrowserWriter) Send(chunk *epb.BrowserFileChunk) error {
	if err := validateBrowserFileChunk(chunk); err != nil {
		return err
	}
	if !s.opened {
		if err := requireExtensionServer(chunk.ServerId); err != nil {
			return err
		}
		s.server, s.profile, s.opened = chunk.ServerId, chunk.ProfileId, true
	} else if chunk.ServerId != "" && chunk.ServerId != s.server || chunk.ProfileId != "" && chunk.ProfileId != s.profile {
		return connect.NewError(connect.CodeInvalidArgument, "browser upload scope changed")
	}
	return s.stream.Send(chunk)
}
func (s *coreBrowserWriter) CloseSend() error {
	if !s.opened {
		return connect.NewError(connect.CodeInvalidArgument, "browser upload required")
	}
	return s.stream.CloseSend()
}
func (s *coreBrowserWriter) Receive() (*epb.BrowserFile, error) {
	defer s.cancel()
	result := new(epb.BrowserFile)
	if err := s.stream.Receive(result); err != nil {
		return nil, err
	}
	return result, nil
}
func (s *coreBrowserWriter) CloseAndReceive() (*epb.BrowserFile, error) {
	if err := s.CloseSend(); err != nil {
		defer s.cancel()
		return nil, err
	}
	return s.Receive()
}
func (s *coreBrowserWriter) Close() error { s.cancel(); return nil }

func (c *coreClient) Region() RegionClient   { return c.region }
func (c *coreClient) Browser() BrowserClient { return c.browser }

var _ RegionClient = (*coreRegionClient)(nil)
var _ BrowserClient = (*coreBrowserClient)(nil)
var _ ExtensionCore = (*coreClient)(nil)

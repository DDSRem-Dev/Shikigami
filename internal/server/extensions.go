package server

import (
	"context"
	"sync"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/DDSRem-Dev/Shikigami/internal/auth"
	sdk "github.com/DDSRem-Dev/Shikigami/pkg/plugin"
	epb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/extensions"
	erpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/extensions/extensionsconnect"
	"google.golang.org/protobuf/proto"
)

// Grants are derived at the authenticated host boundary. Never forward a
// caller-provided owner or DNS/control grant to an independently hosted worker.
func (s *Service) ApplyRegion(ctx context.Context, request *epb.RegionApplyRequest) (*epb.RegionOperation, error) {
	claims, ok := auth.FromContext(ctx)
	if !ok || claims == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, "verified identity required")
	}
	if request == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, "region apply request required")
	}
	request = proto.Clone(request).(*epb.RegionApplyRequest)
	request.DnsAuthorized = claims.HasPermission("region.dns")
	return s.DaemonDataPlane.ApplyRegion(ctx, request)
}

func (s *Service) TakeBrowserControl(ctx context.Context, request *epb.BrowserControlRequest) (*epb.BrowserStatus, error) {
	claims, ok := auth.FromContext(ctx)
	if !ok || claims == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, "verified identity required")
	}
	if !claims.HasPermission("browser.control") {
		return nil, connect.NewError(connect.CodePermissionDenied, "browser control permission required")
	}
	if request == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, "browser control request required")
	}
	request = proto.Clone(request).(*epb.BrowserControlRequest)
	request.OwnerId = claims.ID
	return s.DaemonDataPlane.TakeBrowserControl(ctx, request)
}

func (s *Service) BrowserStream(ctx context.Context, stream erpc.BrowserServiceBrowserStreamServerStream) error {
	claims, ok := auth.FromContext(ctx)
	if !ok || claims == nil || claims.ID == "" {
		return connect.NewError(connect.CodeUnauthenticated, "verified identity required")
	}
	if !claims.HasPermission("browser.view") {
		return connect.NewError(connect.CodePermissionDenied, "browser view permission required")
	}
	transport, ok := ctx.Value(requestReadKey{}).(*requestReadControl)
	if !ok {
		return connect.NewError(connect.CodeFailedPrecondition, "browser transport controls required")
	}
	if err := transport.set(time.Now().Add(5 * time.Second)); err != nil {
		return err
	}
	first, err := stream.Receive()
	if clearErr := transport.set(time.Time{}); err == nil {
		err = clearErr
	}
	if err != nil {
		return err
	}
	if first == nil || first.Kind != epb.BrowserFrame_OPEN || first.ProfileId == "" {
		return connect.NewError(connect.CodeInvalidArgument, "browser OPEN frame required")
	}
	if !first.ReadOnly && !claims.HasPermission("browser.write") {
		return connect.NewError(connect.CodePermissionDenied, "writable browser requires view and write permissions")
	}
	first = proto.Clone(first).(*epb.BrowserFrame)
	first.OwnerId = claims.ID
	first.CanControl = claims.HasPermission("browser.control")
	guarded := &browserGrantStream{stream: stream, first: first, owner: claims.ID, readOnly: first.ReadOnly, canControl: first.CanControl, server: first.ServerId, profile: first.ProfileId, instance: first.InstanceId}
	return s.DaemonDataPlane.BrowserStream(ctx, guarded)
}

type browserGrantStream struct {
	stream     sdk.BrowserServerStream
	first      *epb.BrowserFrame
	owner      string
	readOnly   bool
	canControl bool
	server     string
	profile    string
	mu         sync.Mutex
	instance   string
	attachment string
}

func (s *browserGrantStream) Receive() (*epb.BrowserFrame, error) {
	if s.first != nil {
		first := s.first
		s.first = nil
		return first, nil
	}
	frame, err := s.stream.Receive()
	if err != nil {
		return nil, err
	}
	if frame == nil || frame.Kind == epb.BrowserFrame_OPEN {
		return nil, connect.NewError(connect.CodeInvalidArgument, "browser OPEN cannot be repeated")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if frame.ServerId != "" && frame.ServerId != s.server || frame.ProfileId != "" && frame.ProfileId != s.profile || frame.InstanceId != "" && frame.InstanceId != s.instance || frame.AttachmentId != "" && frame.AttachmentId != s.attachment {
		return nil, connect.NewError(connect.CodePermissionDenied, "browser attachment scope changed")
	}
	if frame.Kind == epb.BrowserFrame_CONTROL && !s.canControl {
		return nil, connect.NewError(connect.CodePermissionDenied, "browser control permission required")
	}
	frame = proto.Clone(frame).(*epb.BrowserFrame)
	frame.OwnerId, frame.CanControl, frame.ReadOnly = s.owner, s.canControl, s.readOnly
	frame.ProfileId, frame.InstanceId, frame.AttachmentId = s.profile, s.instance, s.attachment
	return frame, nil
}

func (s *browserGrantStream) Send(frame *epb.BrowserFrame) error {
	if frame == nil {
		return connect.NewError(connect.CodeDataLoss, "browser response frame missing")
	}
	s.mu.Lock()
	if s.attachment == "" && frame.Kind != epb.BrowserFrame_READY {
		s.mu.Unlock()
		return connect.NewError(connect.CodeDataLoss, "browser READY frame required")
	}
	if frame.ProfileId != s.profile || s.instance != "" && frame.InstanceId != s.instance || s.attachment != "" && frame.AttachmentId != s.attachment {
		s.mu.Unlock()
		return connect.NewError(connect.CodeDataLoss, "browser response scope changed")
	}
	if frame.Kind == epb.BrowserFrame_READY {
		if frame.InstanceId == "" || frame.AttachmentId == "" {
			s.mu.Unlock()
			return connect.NewError(connect.CodeDataLoss, "browser READY scope missing")
		}
		s.instance, s.attachment = frame.InstanceId, frame.AttachmentId
	}
	s.mu.Unlock()
	return s.stream.Send(frame)
}

// The browser server's current-host read is the sole server-side extension
// callback. A plugin gets neither user claims nor arbitrary server routing.
type browserRegionCore struct {
	erpc.UnimplementedRegionServiceHandler
	service *Service
}

func (c *browserRegionCore) InspectRegion(ctx context.Context, request *epb.RegionInspectRequest) (*epb.RegionStatus, error) {
	if request == nil || request.ServerId != "" {
		return nil, connect.NewError(connect.CodePermissionDenied, "current-host region scope required")
	}
	return c.service.DaemonDataPlane.InspectRegion(ctx, request)
}

package plugin

import (
	"context"

	epb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/extensions"
)

const RegionCapability = "region.host.v1"
const BrowserCapability = "browser.remote.v1"
const BrowserLaunchModeCapability = "browser.launch-mode.v1"
const BrowserViewerCapability = "browser.viewer.v1"

// ExtensionLifecycle is optional so existing plugin hosts and fixtures retain
// the original Lifecycle contract. The SDK's native adapter implements both.
type ExtensionLifecycle interface {
	Region() RegionClient
	Browser() BrowserClient
	BrowserLocal() BrowserLocalClient
}

// ExtensionCore exposes only scoped extension callbacks, never a connection.
// The host authorizes each method and server_id for the current plugin instance.
type ExtensionCore interface {
	Region() RegionClient
	Browser() BrowserClient
}

type RegionService interface {
	InspectRegion(context.Context, *epb.RegionInspectRequest) (*epb.RegionStatus, error)
	PlanRegion(context.Context, *epb.RegionPlanRequest) (*epb.RegionPlan, error)
	ApplyRegion(context.Context, *epb.RegionApplyRequest) (*epb.RegionOperation, error)
	ConfirmRegion(context.Context, *epb.RegionOperationRequest) (*epb.RegionOperation, error)
	RollbackRegion(context.Context, *epb.RegionOperationRequest) (*epb.RegionOperation, error)
	GetRegionOperation(context.Context, *epb.RegionOperationRequest) (*epb.RegionOperation, error)
	DiagnoseRegion(context.Context, *epb.DiagnosticRequest) (*epb.DiagnosticReport, error)
}

type RegionClient interface {
	RegionService
}

type browserOperations interface {
	ListBrowserProfiles(context.Context, *epb.BrowserScope) (*epb.BrowserProfileList, error)
	PutBrowserProfile(context.Context, *epb.PutBrowserProfileRequest) (*epb.BrowserProfile, error)
	DeleteBrowserProfile(context.Context, *epb.DeleteBrowserProfileRequest) (*epb.BrowserProfile, error)
	StartBrowser(context.Context, *epb.BrowserActionRequest) (*epb.BrowserStatus, error)
	StopBrowser(context.Context, *epb.BrowserActionRequest) (*epb.BrowserStatus, error)
	GetBrowserStatus(context.Context, *epb.BrowserScope) (*epb.BrowserStatus, error)
	TakeBrowserControl(context.Context, *epb.BrowserControlRequest) (*epb.BrowserStatus, error)
	ListBrowserFiles(context.Context, *epb.BrowserFileRequest) (*epb.BrowserFileList, error)
	DiagnoseBrowser(context.Context, *epb.DiagnosticRequest) (*epb.DiagnosticReport, error)
}

// BrowserService owns browser business policy. Streams carry fixed-purpose
// browser frames; implementations must enforce attachment and control grants.
type BrowserService interface {
	browserOperations
	BrowserStream(context.Context, BrowserServerStream) error
	ReadBrowserFile(context.Context, *epb.BrowserFileRequest, func(*epb.BrowserFileChunk) error) error
	// A canceled receive is terminal, including receives with a new context.
	WriteBrowserFile(context.Context, func(context.Context) (*epb.BrowserFileChunk, error)) (*epb.BrowserFile, error)
}

type BrowserServerStream interface {
	Send(*epb.BrowserFrame) error
	Receive() (*epb.BrowserFrame, error)
}

type BrowserClientStream interface {
	BrowserServerStream
	CloseSend() error
	Close() error
}

type BrowserWriteStream interface {
	Send(*epb.BrowserFileChunk) error
	// Receive observes the single terminal response without closing the request.
	// One sender (including CloseSend) and one receiver may run concurrently.
	CloseSend() error
	Receive() (*epb.BrowserFile, error)
	CloseAndReceive() (*epb.BrowserFile, error)
	Close() error
}

type BrowserClient interface {
	browserOperations
	BrowserStream(context.Context) (BrowserClientStream, error)
	ReadBrowserFile(context.Context, *epb.BrowserFileRequest) (Receiver[epb.BrowserFileChunk], error)
	WriteBrowserFile(context.Context) (BrowserWriteStream, error)
}

// BrowserLocalService belongs to a client plugin, not the daemon listener.
type BrowserLocalService interface {
	OpenBrowserViewer(context.Context, *epb.OpenBrowserViewerRequest) (*epb.BrowserViewer, error)
	CloseBrowserViewer(context.Context, *epb.CloseBrowserViewerRequest) (*epb.BrowserViewer, error)
}

type BrowserLocalClient interface {
	BrowserLocalService
}

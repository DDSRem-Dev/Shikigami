package plugin

import (
	"context"
	"errors"
	"io"
	"sync"

	"connectrpc.com/connect/v2"
	"github.com/DDSRem-Dev/Shikigami/internal/connection"
	sdk "github.com/DDSRem-Dev/Shikigami/pkg/plugin"
	epb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/extensions"
	erpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/extensions/extensionsconnect"
)

func (m *Supervisor) RegionCall(ctx context.Context, fn func(context.Context, sdk.RegionClient) error) error {
	if m == nil {
		return connect.NewError(connect.CodeFailedPrecondition, "region plugin is unavailable")
	}
	return m.Call(ctx, "region", sdk.RegionCapability, func(call context.Context, lifecycle sdk.Lifecycle) error {
		extensions, ok := lifecycle.(sdk.ExtensionLifecycle)
		var client sdk.RegionClient
		if ok {
			client = extensions.Region()
		}
		if client == nil {
			return connect.NewError(connect.CodeFailedPrecondition, "region service is unavailable")
		}
		return fn(call, client)
	})
}

func (m *Supervisor) BrowserCall(ctx context.Context, fn func(context.Context, sdk.BrowserClient) error) error {
	if m == nil {
		return connect.NewError(connect.CodeFailedPrecondition, "browser plugin is unavailable")
	}
	return m.Call(ctx, "browser", sdk.BrowserCapability, func(call context.Context, lifecycle sdk.Lifecycle) error {
		extensions, ok := lifecycle.(sdk.ExtensionLifecycle)
		var client sdk.BrowserClient
		if ok {
			client = extensions.Browser()
		}
		if client == nil {
			return connect.NewError(connect.CodeFailedPrecondition, "browser service is unavailable")
		}
		return fn(call, client)
	})
}

func (m *Supervisor) BrowserLocalCall(ctx context.Context, fn func(context.Context, sdk.BrowserLocalClient) error) error {
	if m == nil {
		return connect.NewError(connect.CodeFailedPrecondition, "browser viewer plugin is unavailable")
	}
	return m.Call(ctx, "browser", sdk.BrowserViewerCapability, func(call context.Context, lifecycle sdk.Lifecycle) error {
		extensions, ok := lifecycle.(sdk.ExtensionLifecycle)
		var client sdk.BrowserLocalClient
		if ok {
			client = extensions.BrowserLocal()
		}
		if client == nil {
			return connect.NewError(connect.CodeFailedPrecondition, "browser viewer service is unavailable")
		}
		return fn(call, client)
	})
}

func (d *DaemonDataPlane) InspectRegion(ctx context.Context, request *epb.RegionInspectRequest) (response *epb.RegionStatus, err error) {
	err = d.Supervisor.RegionCall(ctx, func(call context.Context, client sdk.RegionClient) error {
		response, err = client.InspectRegion(call, request)
		return err
	})
	return response, connection.RebuildError(err)
}
func (d *DaemonDataPlane) PlanRegion(ctx context.Context, request *epb.RegionPlanRequest) (response *epb.RegionPlan, err error) {
	err = d.Supervisor.RegionCall(ctx, func(call context.Context, client sdk.RegionClient) error {
		response, err = client.PlanRegion(call, request)
		return err
	})
	return response, connection.RebuildError(err)
}
func (d *DaemonDataPlane) ApplyRegion(ctx context.Context, request *epb.RegionApplyRequest) (response *epb.RegionOperation, err error) {
	err = d.Supervisor.RegionCall(ctx, func(call context.Context, client sdk.RegionClient) error {
		response, err = client.ApplyRegion(call, request)
		return err
	})
	return response, connection.RebuildError(err)
}
func (d *DaemonDataPlane) ConfirmRegion(ctx context.Context, request *epb.RegionOperationRequest) (response *epb.RegionOperation, err error) {
	err = d.Supervisor.RegionCall(ctx, func(call context.Context, client sdk.RegionClient) error {
		response, err = client.ConfirmRegion(call, request)
		return err
	})
	return response, connection.RebuildError(err)
}
func (d *DaemonDataPlane) RollbackRegion(ctx context.Context, request *epb.RegionOperationRequest) (response *epb.RegionOperation, err error) {
	err = d.Supervisor.RegionCall(ctx, func(call context.Context, client sdk.RegionClient) error {
		response, err = client.RollbackRegion(call, request)
		return err
	})
	return response, connection.RebuildError(err)
}
func (d *DaemonDataPlane) GetRegionOperation(ctx context.Context, request *epb.RegionOperationRequest) (response *epb.RegionOperation, err error) {
	err = d.Supervisor.RegionCall(ctx, func(call context.Context, client sdk.RegionClient) error {
		response, err = client.GetRegionOperation(call, request)
		return err
	})
	return response, connection.RebuildError(err)
}
func (d *DaemonDataPlane) DiagnoseRegion(ctx context.Context, request *epb.DiagnosticRequest) (response *epb.DiagnosticReport, err error) {
	err = d.Supervisor.RegionCall(ctx, func(call context.Context, client sdk.RegionClient) error {
		response, err = client.DiagnoseRegion(call, request)
		return err
	})
	return response, connection.RebuildError(err)
}
func (d *DaemonDataPlane) ListBrowserProfiles(ctx context.Context, request *epb.BrowserScope) (response *epb.BrowserProfileList, err error) {
	err = d.Supervisor.BrowserCall(ctx, func(call context.Context, client sdk.BrowserClient) error {
		response, err = client.ListBrowserProfiles(call, request)
		return err
	})
	return response, connection.RebuildError(err)
}
func (d *DaemonDataPlane) PutBrowserProfile(ctx context.Context, request *epb.PutBrowserProfileRequest) (response *epb.BrowserProfile, err error) {
	err = d.Supervisor.BrowserCall(ctx, func(call context.Context, client sdk.BrowserClient) error {
		response, err = client.PutBrowserProfile(call, request)
		return err
	})
	return response, connection.RebuildError(err)
}
func (d *DaemonDataPlane) DeleteBrowserProfile(ctx context.Context, request *epb.DeleteBrowserProfileRequest) (response *epb.BrowserProfile, err error) {
	err = d.Supervisor.BrowserCall(ctx, func(call context.Context, client sdk.BrowserClient) error {
		response, err = client.DeleteBrowserProfile(call, request)
		return err
	})
	return response, connection.RebuildError(err)
}
func (d *DaemonDataPlane) StartBrowser(ctx context.Context, request *epb.BrowserActionRequest) (response *epb.BrowserStatus, err error) {
	err = d.Supervisor.BrowserCall(ctx, func(call context.Context, client sdk.BrowserClient) error {
		response, err = client.StartBrowser(call, request)
		return err
	})
	return response, connection.RebuildError(err)
}
func (d *DaemonDataPlane) StopBrowser(ctx context.Context, request *epb.BrowserActionRequest) (response *epb.BrowserStatus, err error) {
	err = d.Supervisor.BrowserCall(ctx, func(call context.Context, client sdk.BrowserClient) error {
		response, err = client.StopBrowser(call, request)
		return err
	})
	return response, connection.RebuildError(err)
}
func (d *DaemonDataPlane) GetBrowserStatus(ctx context.Context, request *epb.BrowserScope) (response *epb.BrowserStatus, err error) {
	err = d.Supervisor.BrowserCall(ctx, func(call context.Context, client sdk.BrowserClient) error {
		response, err = client.GetBrowserStatus(call, request)
		return err
	})
	return response, connection.RebuildError(err)
}
func (d *DaemonDataPlane) TakeBrowserControl(ctx context.Context, request *epb.BrowserControlRequest) (response *epb.BrowserStatus, err error) {
	err = d.Supervisor.BrowserCall(ctx, func(call context.Context, client sdk.BrowserClient) error {
		response, err = client.TakeBrowserControl(call, request)
		return err
	})
	return response, connection.RebuildError(err)
}
func (d *DaemonDataPlane) ListBrowserFiles(ctx context.Context, request *epb.BrowserFileRequest) (response *epb.BrowserFileList, err error) {
	err = d.Supervisor.BrowserCall(ctx, func(call context.Context, client sdk.BrowserClient) error {
		response, err = client.ListBrowserFiles(call, request)
		return err
	})
	return response, connection.RebuildError(err)
}
func (d *DaemonDataPlane) DiagnoseBrowser(ctx context.Context, request *epb.DiagnosticRequest) (response *epb.DiagnosticReport, err error) {
	err = d.Supervisor.BrowserCall(ctx, func(call context.Context, client sdk.BrowserClient) error {
		response, err = client.DiagnoseBrowser(call, request)
		return err
	})
	return response, connection.RebuildError(err)
}
func (d *DaemonDataPlane) ReadBrowserFile(ctx context.Context, request *epb.BrowserFileRequest, stream erpc.BrowserServiceReadBrowserFileServerStream) error {
	return d.Supervisor.BrowserCall(ctx, func(call context.Context, client sdk.BrowserClient) error {
		downstream, err := client.ReadBrowserFile(call, request)
		if err != nil {
			return connection.RebuildError(err)
		}
		defer downstream.Close()
		return forwardResponses(downstream, stream.Send)
	})
}
func (d *DaemonDataPlane) WriteBrowserFile(ctx context.Context, stream erpc.BrowserServiceWriteBrowserFileServerStream) (response *epb.BrowserFile, err error) {
	err = d.Supervisor.BrowserCall(ctx, func(call context.Context, client sdk.BrowserClient) error {
		call, cancel := context.WithCancel(call)
		defer cancel()
		downstream, err := client.WriteBrowserFile(call)
		if err != nil {
			return connection.RebuildError(err)
		}
		defer downstream.Close()
		response, err = bridgeBrowserUpload(ctx, cancel, stream.Receive, downstream)
		return err
	})
	return response, connection.RebuildError(err)
}

// A worker may finish while the uploader leaves its request side open. Keep
// terminal reception independent of that sender, then interrupt and join the
// upstream reader before the handler releases its transport and admission slot.
func bridgeBrowserUpload(ctx context.Context, cancel context.CancelFunc, receive func() (*epb.BrowserFileChunk, error), downstream sdk.BrowserWriteStream) (*epb.BrowserFile, error) {
	if _, ok := ctx.Value(cancelReadKey{}).(func()); !ok {
		return nil, connect.NewError(connect.CodeFailedPrecondition, "stream cancellation middleware is required")
	}
	input := make(chan error, 1)
	var sender sync.WaitGroup
	sender.Add(1)
	go func() {
		defer sender.Done()
		for {
			chunk, err := receive()
			if errors.Is(err, io.EOF) {
				err = downstream.CloseSend()
			} else if err == nil {
				err = downstream.Send(chunk)
				if err == nil {
					continue
				}
			}
			input <- err
			if err != nil && !errors.Is(err, io.EOF) {
				cancel()
				_ = downstream.Close()
			}
			return
		}
	}()
	response, outputErr := downstream.Receive()
	var inputErr error
	inputFinished := false
	select {
	case inputErr = <-input:
		inputFinished = true
	default:
	}
	cancel()
	_ = downstream.Close()
	closeInput(ctx)
	if !inputFinished {
		inputErr = <-input
	}
	sender.Wait()
	if inputFinished && inputErr != nil && !errors.Is(inputErr, io.EOF) && !errors.Is(inputErr, context.Canceled) && (outputErr == nil || connect.CodeOf(outputErr) == connect.CodeCanceled) {
		return nil, connection.RebuildError(inputErr)
	}
	return response, connection.RebuildError(outputErr)
}
func (d *DaemonDataPlane) BrowserStream(ctx context.Context, stream sdk.BrowserServerStream) error {
	return d.Supervisor.BrowserCall(ctx, func(call context.Context, client sdk.BrowserClient) error {
		call, cancel := context.WithCancel(call)
		defer cancel()
		downstream, err := client.BrowserStream(call)
		if err != nil {
			return connection.RebuildError(err)
		}
		defer downstream.Close()
		return bridgeBrowser(ctx, cancel, stream.Receive, stream.Send, downstream)
	})
}

func bridgeBrowser(ctx context.Context, cancel context.CancelFunc, receive func() (*epb.BrowserFrame, error), send func(*epb.BrowserFrame) error, downstream sdk.BrowserClientStream) error {
	if _, ok := ctx.Value(cancelReadKey{}).(func()); !ok {
		return connect.NewError(connect.CodeFailedPrecondition, "stream cancellation middleware is required")
	}
	input := make(chan error, 1)
	go func() {
		for {
			frame, err := receive()
			if errors.Is(err, io.EOF) {
				input <- downstream.CloseSend()
				return
			}
			if err == nil {
				err = downstream.Send(frame)
			}
			if err != nil {
				input <- err
				if !errors.Is(err, io.EOF) {
					cancel()
					_ = downstream.Close()
				}
				return
			}
		}
	}()
	var outputErr error
	for {
		frame, err := downstream.Receive()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			outputErr = err
			break
		}
		if err = send(frame); err != nil {
			outputErr = err
			break
		}
	}
	cancel()
	_ = downstream.Close()
	closeInput(ctx)
	inputErr := <-input
	if inputErr != nil && !errors.Is(inputErr, io.EOF) && !errors.Is(inputErr, context.Canceled) && (outputErr == nil || connect.CodeOf(outputErr) == connect.CodeCanceled) {
		return connection.RebuildError(inputErr)
	}
	return connection.RebuildError(outputErr)
}

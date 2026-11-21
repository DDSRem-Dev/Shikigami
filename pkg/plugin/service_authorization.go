package plugin

import (
	"context"
	"errors"

	epb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/extensions"
	ppb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func validateServiceRegistration(side string, info *ppb.PluginInfo, services Services) error {
	if side != "server" && (services.Files != nil || services.Ports != nil || services.Region != nil || services.Browser != nil) {
		return errors.New("server data services cannot run on a client plugin")
	}
	if side != "client" && (services.Sync != nil || services.Port != nil || services.BrowserLocal != nil) {
		return errors.New("local worker requires client side")
	}
	if info == nil || info.Protocol == nil {
		return errors.New("plugin protocol offer is required")
	}
	capabilities := make(map[string]bool, len(info.Protocol.Capabilities))
	for _, capability := range info.Protocol.Capabilities {
		capabilities[capability] = true
	}
	if capabilities[BrowserLaunchModeCapability] && (side != "server" || services.Browser == nil || !capabilities[BrowserCapability]) {
		return errors.New("browser launch mode capability requires the server browser service")
	}
	if side == "server" {
		if (services.Files != nil) != capabilities[FileCapability] || (services.Ports != nil) != capabilities[PortCapability] ||
			(services.Region != nil) != capabilities[RegionCapability] || (services.Browser != nil) != capabilities[BrowserCapability] {
			return errors.New("server capabilities must match registered services")
		}
	}
	if side == "client" {
		if (services.Sync != nil) != capabilities[SyncCapability] || (services.Port != nil) != capabilities[PortWorkerCapability] ||
			(services.BrowserLocal != nil) != capabilities[BrowserViewerCapability] {
			return errors.New("client capabilities must match registered workers")
		}
	}
	return nil
}

// registeredServiceMethods is deliberately explicit: adding a proto method
// never grants access until its native service registration is updated here.
func registeredServiceMethods(services Services) map[string]bool {
	methods := map[string]bool{ppb.Plugin_InvokeAction_FullMethodName: true}
	if services.Port != nil {
		for _, method := range []string{ppb.PortWorker_DiscoverForwards_FullMethodName, ppb.PortWorker_StartForward_FullMethodName, ppb.PortWorker_StopForward_FullMethodName, ppb.PortWorker_ListForwards_FullMethodName} {
			methods[method] = true
		}
	}
	if services.Files != nil {
		for _, method := range []string{ppb.FileService_ListFiles_FullMethodName, ppb.FileService_StatFile_FullMethodName, ppb.FileService_ReadFile_FullMethodName, ppb.FileService_WriteFile_FullMethodName, ppb.FileService_DeleteFile_FullMethodName, ppb.FileService_WatchFiles_FullMethodName} {
			methods[method] = true
		}
	}
	if services.Ports != nil {
		for _, method := range []string{ppb.PortService_CheckPort_FullMethodName, ppb.PortService_ListPorts_FullMethodName, ppb.PortService_WatchPorts_FullMethodName, ppb.PortService_ForwardPort_FullMethodName} {
			methods[method] = true
		}
	}
	if services.Sync != nil {
		for _, method := range []string{ppb.SyncWorker_ConfigureMapping_FullMethodName, ppb.SyncWorker_GetSyncState_FullMethodName, ppb.SyncWorker_ListConflicts_FullMethodName, ppb.SyncWorker_ResolveConflict_FullMethodName, ppb.SyncWorker_ReadConflictFile_FullMethodName} {
			methods[method] = true
		}
	}
	if services.Region != nil {
		for _, method := range []string{epb.RegionService_InspectRegion_FullMethodName, epb.RegionService_PlanRegion_FullMethodName, epb.RegionService_ApplyRegion_FullMethodName, epb.RegionService_ConfirmRegion_FullMethodName, epb.RegionService_RollbackRegion_FullMethodName, epb.RegionService_GetRegionOperation_FullMethodName, epb.RegionService_DiagnoseRegion_FullMethodName} {
			methods[method] = true
		}
	}
	if services.Browser != nil {
		for _, method := range []string{epb.BrowserService_ListBrowserProfiles_FullMethodName, epb.BrowserService_PutBrowserProfile_FullMethodName, epb.BrowserService_DeleteBrowserProfile_FullMethodName, epb.BrowserService_StartBrowser_FullMethodName, epb.BrowserService_StopBrowser_FullMethodName, epb.BrowserService_GetBrowserStatus_FullMethodName, epb.BrowserService_TakeBrowserControl_FullMethodName, epb.BrowserService_BrowserStream_FullMethodName, epb.BrowserService_ListBrowserFiles_FullMethodName, epb.BrowserService_ReadBrowserFile_FullMethodName, epb.BrowserService_WriteBrowserFile_FullMethodName, epb.BrowserService_DiagnoseBrowser_FullMethodName} {
			methods[method] = true
		}
	}
	if services.BrowserLocal != nil {
		for _, method := range []string{epb.BrowserLocalService_OpenBrowserViewer_FullMethodName, epb.BrowserLocalService_CloseBrowserViewer_FullMethodName} {
			methods[method] = true
		}
	}
	return methods
}

func (r *runtime) unaryAuthorization(methods map[string]bool) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, next grpc.UnaryHandler) (any, error) {
		switch info.FullMethod {
		case "/grpc.health.v1.Health/Check", "/plugin.GRPCController/Shutdown", ppb.Plugin_GetInfo_FullMethodName, ppb.Plugin_Init_FullMethodName:
		case ppb.Plugin_Start_FullMethodName, ppb.Plugin_Stop_FullMethodName, ppb.Plugin_Health_FullMethodName:
			if !r.authorized(ctx) {
				return nil, status.Error(codes.Unauthenticated, "plugin instance authentication required")
			}
		default:
			if !methods[info.FullMethod] {
				return nil, status.Error(codes.PermissionDenied, "plugin method not permitted")
			}
			if !r.authorized(ctx) {
				return nil, status.Error(codes.Unauthenticated, "plugin instance authentication required")
			}
		}
		return next(ctx, req)
	}
}

func (r *runtime) streamAuthorization(methods map[string]bool) grpc.StreamServerInterceptor {
	return func(srv any, stream grpc.ServerStream, info *grpc.StreamServerInfo, next grpc.StreamHandler) error {
		switch info.FullMethod {
		case "/plugin.GRPCBroker/StartStream", "/plugin.GRPCStdio/StreamStdio":
			return next(srv, stream)
		default:
			if !methods[info.FullMethod] {
				return status.Error(codes.PermissionDenied, "plugin stream not permitted")
			}
			if !r.authorized(stream.Context()) {
				return status.Error(codes.Unauthenticated, "plugin instance authentication required")
			}
			return next(srv, stream)
		}
	}
}

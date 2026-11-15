package auth

import (
	"errors"
	"testing"

	erpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/extensions/extensionsconnect"
)

func TestExtensionPermissionsAreExplicitAndIndependent(t *testing.T) {
	cases := []struct {
		procedure string
		grant     string
	}{
		{erpc.RegionServiceInspectRegionProcedure, "region.read"},
		{erpc.RegionServicePlanRegionProcedure, "region.plan"},
		{erpc.RegionServiceApplyRegionProcedure, "region.apply"},
		{erpc.RegionServiceRollbackRegionProcedure, "region.rollback"},
		{erpc.RegionServiceGetRegionOperationProcedure, "region.read"},
		{erpc.RegionServiceDiagnoseRegionProcedure, "region.read"},
		{erpc.BrowserServiceListBrowserProfilesProcedure, "browser.list"},
		{erpc.BrowserServicePutBrowserProfileProcedure, "browser.manage"},
		{erpc.BrowserServiceDeleteBrowserProfileProcedure, "browser.manage"},
		{erpc.BrowserServiceStartBrowserProcedure, "browser.manage"},
		{erpc.BrowserServiceStopBrowserProcedure, "browser.manage"},
		{erpc.BrowserServiceGetBrowserStatusProcedure, "browser.view"},
		{erpc.BrowserServiceTakeBrowserControlProcedure, "browser.control"},
		{erpc.BrowserServiceBrowserStreamProcedure, "browser.view"},
		{erpc.BrowserServiceListBrowserFilesProcedure, "browser.files.read"},
		{erpc.BrowserServiceReadBrowserFileProcedure, "browser.files.read"},
		{erpc.BrowserServiceWriteBrowserFileProcedure, "browser.files.write"},
		{erpc.BrowserServiceDiagnoseBrowserProcedure, "browser.diagnose"},
	}
	for _, test := range cases {
		t.Run(test.procedure, func(t *testing.T) {
			if err := Authorize(&Claims{Permissions: []string{test.grant}}, test.procedure); err != nil {
				t.Fatal("exact grant rejected", err)
			}
			if err := Authorize(&Claims{Permissions: []string{"system.*"}}, test.procedure); !errors.Is(err, ErrPermissionDenied) {
				t.Fatal("unrelated permission admitted extension", err)
			}
		})
	}
	view := &Claims{Permissions: []string{"browser.view"}}
	if view.HasPermission("browser.write") || view.HasPermission("browser.control") {
		t.Fatal("view silently expanded to write or control")
	}
	control := &Claims{Permissions: []string{"browser.control"}}
	if control.HasPermission("browser.write") || control.HasPermission("browser.view") {
		t.Fatal("control silently expanded to write or view")
	}
	if err := Authorize(&Claims{Permissions: []string{"region.apply"}}, erpc.RegionServiceConfirmRegionProcedure); !errors.Is(err, ErrPermissionDenied) {
		t.Fatal("DNS confirmation did not require independent DNS grant", err)
	}
	if err := Authorize(&Claims{Permissions: []string{"region.apply", "region.dns"}}, erpc.RegionServiceConfirmRegionProcedure); err != nil {
		t.Fatal(err)
	}
	admin := &Claims{Permissions: []string{"*"}}
	for _, procedure := range []string{"/shiki.extensions.v1.BrowserService/FutureMethod", erpc.BrowserLocalServiceOpenBrowserViewerProcedure, "/unexpected.BrowserService/BrowserStream"} {
		if err := Authorize(admin, procedure); !errors.Is(err, ErrPermissionDenied) {
			t.Fatal("unknown or local-only remote method admitted", procedure, err)
		}
	}
}

//go:build !linux

package helper

import (
	"context"
	"io"
	"time"

	epb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/extensions"
)

func Run(_ context.Context, _ []string, _ io.Reader, stdout, _ io.Writer) int {
	_ = EncodeResponse(stdout, Response{Code: "failed_precondition", Error: "region root helper supports Ubuntu 24.04 and Debian 12 Linux only"})
	return 1
}

func (Client) Call(context.Context, string, Request) (Response, error) {
	return Response{}, fail("failed_precondition", "region root helper is Linux-only")
}
func InspectReadOnly(context.Context) (*epb.RegionStatus, error) {
	return &epb.RegionStatus{HelperAvailable: false, State: "unsupported_platform", ObservedAt: time.Now().Unix(), Warnings: []string{"Region host mutation supports Ubuntu 24.04 and Debian 12 Linux only; no host settings changed"}}, nil
}

package plugin

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connectproto"
	rpcerrors "github.com/DDSRem-Dev/Shikigami/pkg/errors"
	"github.com/DDSRem-Dev/Shikigami/pkg/protocol/version"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestTargetRefusedSurvivesWorkerBoundaries(t *testing.T) {
	err := error(PortTargetRefused())
	for range 3 {
		err = FromGRPC(ToGRPC(err))
		if !IsPortTargetRefused(err) || connect.CodeOf(err) != connect.CodeUnavailable {
			t.Fatalf("lost marker: %v", err)
		}
	}
	info := &errdetails.ErrorInfo{Domain: "shiki.port", Reason: "TARGET_CONNECTION_REFUSED", Metadata: map[string]string{"target": "private"}}
	detail, _ := connectproto.NewErrorDetail(info)
	unsafe := connect.NewError(connect.CodeUnavailable, "private").WithDetail(detail)
	for _, bad := range []error{unsafe, connect.NewError(connect.CodeUnavailable, "TARGET_CONNECTION_REFUSED")} {
		rebuilt := FromGRPC(ToGRPC(bad))
		if IsPortTargetRefused(rebuilt) || rpcerrors.SafeDetail(rebuilt) != nil || strings.Contains(rebuilt.Error(), "private") {
			t.Fatalf("unsafe marker: %v", rebuilt)
		}
	}
	remote, _ := status.New(codes.Unavailable, "private").WithDetails(info)
	if IsPortTargetRefused(FromGRPC(remote.Err())) {
		t.Fatal("untrusted worker metadata accepted")
	}
	protocol := version.MissingRemoteCapability("port.forward.v1")
	if version.SafeDetail(FromGRPC(ToGRPC(protocol))) == nil {
		t.Fatal("lost existing protocol detail")
	}
}

func TestTargetRefusedCancellationSurvivesWorkerBoundaries(t *testing.T) {
	for _, sentinel := range []error{context.Canceled, context.DeadlineExceeded} {
		want := connect.CodeCanceled
		if sentinel == context.DeadlineExceeded {
			want = connect.CodeDeadlineExceeded
		}
		for name, err := range map[string]error{
			"cause":               PortTargetRefused().WithCause(sentinel),
			"wrapped":             fmt.Errorf("outer: %w", PortTargetRefused().WithCause(sentinel)),
			"joined-marker-first": errors.Join(PortTargetRefused(), sentinel),
			"joined-cancel-first": errors.Join(sentinel, PortTargetRefused()),
		} {
			t.Run(sentinel.Error()+"/"+name, func(t *testing.T) {
				if IsPortTargetRefused(err) {
					t.Fatal("SDK marker masked cancellation")
				}
				for range 3 {
					err = FromGRPC(ToGRPC(err))
					var failure *connect.Error
					if !errors.As(err, &failure) || failure.Code() != want || len(failure.Details()) != 0 || IsPortTargetRefused(err) {
						t.Fatalf("lost cancellation or retained marker: %v", err)
					}
				}
			})
		}
	}
}

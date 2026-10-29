package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"path/filepath"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	sdk "github.com/DDSRem-Dev/Shikigami/pkg/plugin"
	"github.com/DDSRem-Dev/Shikigami/pkg/plugin/limits"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	ppb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin"
)

// No CoreAPI call is needed before an upload supplies its first header.
type admissionCore struct{ sdk.CoreAPI }

func startedAdmissionWorker(t *testing.T) *Worker {
	t.Helper()
	w := New()
	if err := w.Init(context.Background(), sdk.InitContext{Core: admissionCore{}, Side: "server", StateDir: filepath.Join(t.TempDir(), "state")}); err != nil {
		t.Fatal(err)
	}
	if err := w.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := w.Stop(ctx); err != nil {
			t.Errorf("stop worker: %v", err)
		}
	})
	return w
}

func TestFileAdmissionSeparatesClassesAndReleasesCapacity(t *testing.T) {
	w := startedAdmissionWorker(t)
	for _, class := range []operationClass{metadataOperation, transferOperation, watchOperation} {
		var releases []func()
		defer func() {
			for _, release := range releases {
				release()
			}
		}()
		for i := 0; i < operationLimits[class]; i++ {
			_, release, err := w.begin(context.Background(), class)
			if err != nil {
				t.Fatal(err)
			}
			releases = append(releases, release)
		}
		// Acquire no filesystem or goroutine resources to fill the budget.
		// Exercise actual method entrypoints while the class is full.
		var errs []error
		switch class {
		case metadataOperation:
			_, listErr := w.ListFiles(context.Background(), &pb.ListFilesRequest{})
			_, statErr := w.StatFile(context.Background(), &pb.StatFileRequest{})
			errs = []error{listErr, statErr, w.DeleteFile(context.Background(), &pb.DeleteFileRequest{})}
		case transferOperation:
			readErr := w.ReadFile(context.Background(), &pb.ReadFileRequest{}, nil)
			_, writeErr := w.WriteFile(context.Background(), func(context.Context) (*pb.WriteFileRequest, error) {
				t.Error("over-budget upload read transport input")
				return nil, io.EOF
			})
			errs = []error{readErr, writeErr}
		case watchOperation:
			errs = []error{w.WatchFiles(context.Background(), &pb.WatchFilesRequest{}, nil)}
		}
		for _, err := range errs {
			if connect.CodeOf(err) != connect.CodeResourceExhausted {
				t.Errorf("full class %d: %v", class, err)
			}
		}
		for _, other := range []operationClass{metadataOperation, transferOperation, watchOperation} {
			if other != class {
				_, release, err := w.begin(context.Background(), other)
				if err != nil {
					t.Fatal("one class blocked another:", err)
				}
				release()
			}
		}
		for _, release := range releases {
			release()
		}
		releases = nil
		_, release, err := w.begin(context.Background(), class)
		if err != nil {
			t.Fatal("released capacity unavailable:", err)
		}
		release()
	}
}

func TestFileStopRetryStillWaitsForActiveOperation(t *testing.T) {
	w := startedAdmissionWorker(t)
	ctx, release, err := w.begin(context.Background(), metadataOperation)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	for i := 0; i < 2; i++ {
		stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
		err := w.Stop(stopCtx)
		cancel()
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("Stop %d bypassed pending operation: %v", i, err)
		}
	}
	if ctx.Err() != context.Canceled {
		t.Fatal("Stop failed to cancel admitted context")
	}
}

type admissionProjectCore struct {
	sdk.CoreAPI
	project *pb.Project
}

func (c admissionProjectCore) GetProject(context.Context, *ppb.Scope) (*pb.Project, error) {
	return c.project, nil
}

func TestWriteFileHeaderBudgetDoesNotBecomeTransferDeadline(t *testing.T) {
	w := startedAdmissionWorker(t)
	w.core = admissionProjectCore{project: &pb.Project{Id: "proj_0123456789abcdef", Path: t.TempDir()}}
	emptyHash := sha256.Sum256(nil)
	first := true
	_, err := w.WriteFile(context.Background(), func(ctx context.Context) (*pb.WriteFileRequest, error) {
		if first {
			first = false
			deadline, ok := ctx.Deadline()
			if !ok || time.Until(deadline) > limits.FileFirstFrameTimeout || time.Until(deadline) <= 0 {
				t.Error("missing bounded first-header deadline")
			}
			return &pb.WriteFileRequest{Payload: &pb.WriteFileRequest_Header{Header: &pb.WriteFileHeader{ProjectId: "proj_0123456789abcdef", Path: "empty", ContentHash: hex.EncodeToString(emptyHash[:]), Mode: 0600}}}, nil
		}
		if _, ok := ctx.Deadline(); ok {
			t.Error("first-header deadline leaked into data transfer")
		}
		if ctx.Err() != nil {
			t.Error("canceling header budget canceled transfer context")
		}
		return nil, io.EOF
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestWriteFileHeaderRetainsEarlierParentDeadline(t *testing.T) {
	w := startedAdmissionWorker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	parentDeadline, _ := ctx.Deadline()
	_, err := w.WriteFile(ctx, func(receiveCtx context.Context) (*pb.WriteFileRequest, error) {
		deadline, ok := receiveCtx.Deadline()
		if !ok || !deadline.Equal(parentDeadline) {
			t.Error("first header extended parent deadline")
		}
		<-receiveCtx.Done()
		return nil, receiveCtx.Err()
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("header wait = %v; want deadline exceeded", err)
	}
}

func TestWriteFileChecksLifecycleBeforeReceiving(t *testing.T) {
	for _, state := range []string{"not started", "stopped"} {
		t.Run(state, func(t *testing.T) {
			w := New()
			if state == "stopped" {
				w = startedAdmissionWorker(t)
				if err := w.Stop(context.Background()); err != nil {
					t.Fatal(err)
				}
			}
			received := false
			_, err := w.WriteFile(context.Background(), func(context.Context) (*pb.WriteFileRequest, error) {
				received = true
				return nil, io.EOF
			})
			if received {
				t.Error("upload consumed transport input before rejecting non-running worker")
			}
			if connect.CodeOf(err) != connect.CodeFailedPrecondition {
				t.Errorf("non-running worker error = %v; want failed_precondition", err)
			}
		})
	}
}

func TestWriteFileStopCancelsInitialReceive(t *testing.T) {
	w := startedAdmissionWorker(t)
	entered := make(chan struct{})
	receiveDone := make(chan struct{})
	writeDone := make(chan error, 1)
	requestDone := make(chan struct{})
	// The SDK receiver observes the supplied context. Join the callback and
	// request explicitly; real gRPC transport cleanup is tested in the SDK.
	t.Cleanup(func() {
		select {
		case <-receiveDone:
		case <-time.After(time.Second):
			t.Error("receive callback did not stop after transport cleanup")
		}
		select {
		case <-requestDone:
		case <-time.After(time.Second):
			t.Error("upload did not stop after transport cleanup")
		}
	})
	go func() {
		defer close(requestDone)
		_, err := w.WriteFile(context.Background(), func(ctx context.Context) (*pb.WriteFileRequest, error) {
			close(entered)
			defer close(receiveDone)
			<-ctx.Done()
			return nil, ctx.Err()
		})
		writeDone <- err
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("upload did not enter initial receive")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := w.Stop(ctx); err != nil {
		t.Errorf("stop failed to cancel admitted upload: %v", err)
	}
	select {
	case err := <-writeDone:
		if !errors.Is(err, context.Canceled) && connect.CodeOf(err) != connect.CodeCanceled {
			t.Errorf("stopped upload error = %v; want canceled", err)
		}
	case <-time.After(time.Second):
		t.Error("Stop returned while pre-header WriteFile remained blocked")
	}
}

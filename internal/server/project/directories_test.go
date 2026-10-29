package project

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"connectrpc.com/connect/v2"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	"golang.org/x/sys/unix"
)

func TestDirectoryPickerConfinesAndVersionsPages(t *testing.T) {
	f := setup(t)
	p := create(t, f, "picker")
	for _, name := range []string{"alpha/nested", "beta", "gamma"} {
		if err := os.MkdirAll(filepath.Join(p.Path, name), 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(p.Path, "file"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), filepath.Join(p.Path, "link")); err != nil {
		t.Fatal(err)
	}
	if err := unix.Mkfifo(filepath.Join(p.Path, "fifo"), 0600); err != nil {
		t.Fatal(err)
	}
	request := &pb.ListProjectDirectoriesRequest{ProjectId: p.Id, PageSize: 2}
	first, err := f.manager.ListDirectories(t.Context(), request)
	if err != nil || !reflect.DeepEqual(first.GetDirectories(), []string{"alpha", "beta"}) || first.NextPageToken == "" {
		t.Fatalf("first page: %v %v", first, err)
	}
	request.PageToken = first.NextPageToken
	last, err := f.manager.ListDirectories(t.Context(), request)
	if err != nil || !reflect.DeepEqual(last.GetDirectories(), []string{"gamma"}) || last.NextPageToken != "" {
		t.Fatalf("last page: %v %v", last, err)
	}
	nested, err := f.manager.ListDirectories(t.Context(), &pb.ListProjectDirectoriesRequest{ProjectId: p.Id, Path: "alpha"})
	if err != nil || !reflect.DeepEqual(nested.GetDirectories(), []string{"alpha/nested"}) {
		t.Fatalf("relative choices: %v %v", nested, err)
	}
	for _, relative := range []string{"/", "..", "alpha/..", "alpha//nested", "alpha/", "link", "link/child", "fifo", "file"} {
		_, err := f.manager.ListDirectories(t.Context(), &pb.ListProjectDirectoriesRequest{ProjectId: p.Id, Path: relative})
		if connect.CodeOf(err) != connect.CodeInvalidArgument {
			t.Errorf("unsafe path %q: %v", relative, err)
		}
	}
	if err := os.Mkdir(filepath.Join(p.Path, "delta"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := f.manager.ListDirectories(t.Context(), request); connect.CodeOf(err) != connect.CodeAborted {
		t.Fatalf("stale page accepted: %v", err)
	}
	request.Path = "alpha"
	if _, err := f.manager.ListDirectories(t.Context(), request); connect.CodeOf(err) != connect.CodeAborted {
		t.Fatalf("cross-scope page accepted: %v", err)
	}
	if err := os.Rename(p.Path, p.Path+"-original"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), p.Path); err != nil {
		t.Fatal(err)
	}
	if _, err := f.manager.ListDirectories(t.Context(), &pb.ListProjectDirectoriesRequest{ProjectId: p.Id}); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("replaced project root followed: %v", err)
	}
}

func TestDirectoryPickerLimitsAndInvalidNames(t *testing.T) {
	f := setup(t)
	p := create(t, f, "limits")
	for _, request := range []*pb.ListProjectDirectoriesRequest{{ProjectId: p.Id, PageSize: 101}, {ProjectId: p.Id, PageToken: "not-a-cursor"}} {
		if _, err := f.manager.ListDirectories(t.Context(), request); connect.CodeOf(err) != connect.CodeInvalidArgument {
			t.Fatal(err)
		}
	}
	bad := filepath.Join(p.Path, string([]byte{'b', 0xff}))
	if err := os.Mkdir(bad, 0700); err == nil {
		if _, err := f.manager.ListDirectories(t.Context(), &pb.ListProjectDirectoriesRequest{ProjectId: p.Id}); connect.CodeOf(err) != connect.CodeFailedPrecondition {
			t.Fatalf("invalid UTF8 name accepted: %v", err)
		}
		if err := os.Remove(bad); err != nil {
			t.Fatal(err)
		}
	} else if errors.Is(err, unix.EILSEQ) {
		t.Log("filesystem rejects invalid UTF8 names before directory enumeration")
	} else {
		t.Fatal(err)
	}
	unsupported := filepath.Join(p.Path, `back\slash`)
	if err := os.Mkdir(unsupported, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := f.manager.ListDirectories(t.Context(), &pb.ListProjectDirectoriesRequest{ProjectId: p.Id}); connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("unusable directory choice returned: %v", err)
	}
	if err := os.Remove(unsupported); err != nil {
		t.Fatal(err)
	}
	for i := 0; i <= maxDirectoryEntries; i++ {
		if err := os.WriteFile(filepath.Join(p.Path, fmt.Sprintf("file-%05d", i)), nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.manager.ListDirectories(t.Context(), &pb.ListProjectDirectoriesRequest{ProjectId: p.Id}); connect.CodeOf(err) != connect.CodeResourceExhausted {
		t.Fatalf("unbounded scan: %v", err)
	}
}

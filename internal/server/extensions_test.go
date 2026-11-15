package server

import (
	"io"
	"testing"

	"connectrpc.com/connect/v2"
	epb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/extensions"
)

type browserGrantProbe struct {
	input  []*epb.BrowserFrame
	output []*epb.BrowserFrame
}

func (s *browserGrantProbe) Receive() (*epb.BrowserFrame, error) {
	if len(s.input) == 0 {
		return nil, io.EOF
	}
	frame := s.input[0]
	s.input = s.input[1:]
	return frame, nil
}
func (s *browserGrantProbe) Send(frame *epb.BrowserFrame) error {
	s.output = append(s.output, frame)
	return nil
}

func TestBrowserHostOverwritesEveryFrameGrantAndPinsScope(t *testing.T) {
	probe := &browserGrantProbe{input: []*epb.BrowserFrame{
		{Kind: epb.BrowserFrame_DATA, OwnerId: "forged", CanControl: true, ReadOnly: false, Data: []byte("rfb")},
		{Kind: epb.BrowserFrame_CONTROL, OwnerId: "forged", CanControl: true},
		{Kind: epb.BrowserFrame_DATA, ProfileId: "other"},
	}}
	stream := &browserGrantStream{stream: probe, owner: "authenticated-token", readOnly: true, server: "srv-a", profile: "profile-a"}
	if err := stream.Send(&epb.BrowserFrame{Kind: epb.BrowserFrame_READY, ProfileId: "profile-a", InstanceId: "instance-a", AttachmentId: "attachment-a"}); err != nil {
		t.Fatal(err)
	}
	frame, err := stream.Receive()
	if err != nil || frame.OwnerId != "authenticated-token" || frame.CanControl || !frame.ReadOnly || frame.ProfileId != "profile-a" || frame.InstanceId != "instance-a" || frame.AttachmentId != "attachment-a" {
		t.Fatalf("host grant not authoritative: %+v %v", frame, err)
	}
	if probe.input == nil {
		t.Fatal("probe unexpectedly consumed every frame")
	}
	if _, err := stream.Receive(); connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatal("forged control grant accepted", err)
	}
	if _, err := stream.Receive(); connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatal("profile switch accepted", err)
	}
	if err := stream.Send(&epb.BrowserFrame{Kind: epb.BrowserFrame_DATA, ProfileId: "profile-a", InstanceId: "instance-b", AttachmentId: "attachment-a"}); connect.CodeOf(err) != connect.CodeDataLoss {
		t.Fatal("changed runtime identity accepted", err)
	}
}

func TestBrowserHostRequiresScopedReadyBeforeData(t *testing.T) {
	stream := &browserGrantStream{stream: new(browserGrantProbe), profile: "profile-a"}
	if err := stream.Send(&epb.BrowserFrame{Kind: epb.BrowserFrame_DATA, ProfileId: "profile-a"}); connect.CodeOf(err) != connect.CodeDataLoss {
		t.Fatal("data before READY accepted", err)
	}
	if err := stream.Send(&epb.BrowserFrame{Kind: epb.BrowserFrame_READY, ProfileId: "profile-a"}); connect.CodeOf(err) != connect.CodeDataLoss {
		t.Fatal("READY without identity accepted", err)
	}
}

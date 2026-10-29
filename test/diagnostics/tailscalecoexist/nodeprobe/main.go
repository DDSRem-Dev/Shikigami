// The diagnostic never calls BeginLogin or exports login URLs/node keys.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/DDSRem-Dev/Shikigami/internal/client"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
)

func probe() error {
	if len(os.Args) != 2 || os.Getuid() != 1001 {
		return errors.New("private UID1001 socket argument required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	node, err := client.OpenNode(ctx, os.Args[1], 5*time.Second)
	if err != nil {
		return fmt.Errorf("private NodeControl negotiation failed: %s", connect.CodeOf(err))
	}
	defer node.Close()
	hello, err := node.API().Negotiate(ctx, &pb.ProtocolOffer{Major: 1, Capabilities: []string{"node.login.v1"}, RequiredCapabilities: []string{"node.login.v1"}})
	if err != nil {
		return fmt.Errorf("NodeControl identity read failed: %s", connect.CodeOf(err))
	}
	status, err := node.API().GetStatus(ctx, &pb.Empty{})
	if err != nil {
		return fmt.Errorf("NodeControl status read failed: %s", connect.CodeOf(err))
	}
	if hello.InstanceId == "" || status.LoginId != "" || status.LoginUrl != "" || len(status.Addresses) != 0 {
		return errors.New("node unexpectedly authenticated or initiated login")
	}
	if !(status.State == "needs_login" && status.ReasonCode == "NODE_AUTH_REQUIRED") && !(status.State == "offline" && status.ReasonCode == "NODE_OFFLINE") {
		return fmt.Errorf("node has not settled to unauthenticated state: state=%q reason=%q", status.State, status.ReasonCode)
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]any{"uid": os.Getuid(), "instance_id": hello.InstanceId, "build": hello.Protocol.BuildVersion, "state": status.State, "reason_code": status.ReasonCode, "login_attempt": false, "addresses": 0})
}
func main() {
	if err := probe(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

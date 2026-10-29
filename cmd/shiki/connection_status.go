package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/DDSRem-Dev/Shikigami/internal/client"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	apb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent"
	"google.golang.org/protobuf/proto"
)

type serverStatusReader interface {
	ListServers(context.Context, *pb.Empty) (*apb.ServerList, error)
}

// awaitServerReady observes the one episode triggered by UpsertServer. It sends
// no mutations or business probes and requires the agent's persisted identity
// plus authenticated agreement, so a restricted Token needs no system.info.
func awaitServerReady(ctx context.Context, source serverStatusReader, expected *apb.ServerRecord) (*apb.ServerRecord, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		list, err := source.ListServers(ctx, &pb.Empty{})
		if err != nil {
			return nil, err
		}
		var found *apb.ServerRecord
		for _, record := range list.Servers {
			if record.GetConfig().GetId() == expected.GetConfig().GetId() {
				found = record
				break
			}
		}
		if found == nil {
			return nil, errors.New("server was removed while waiting for verification")
		}
		observedConfig := proto.Clone(found.Config).(*apb.ServerConfig)
		if expected.Config.DaemonId == "" {
			observedConfig.DaemonId = "" // The first verified connection pins this field.
		}
		if !proto.Equal(expected.Config, observedConfig) || expected.CredentialRevision != found.CredentialRevision || !found.CredentialPresent {
			return nil, errors.New("server settings or credentials changed while waiting; retry the explicit connection")
		}
		if found.ConnectionState == "ready" {
			agreement := found.RemoteProtocol
			if !strings.HasPrefix(found.Config.DaemonId, "daemon_") || len(found.Config.DaemonId) <= len("daemon_") || agreement == nil || agreement.Major != 1 || agreement.Minor != 0 || agreement.DaemonId != "" && agreement.DaemonId != found.Config.DaemonId {
				return nil, errors.New("agent reported ready without a matching authenticated protocol and pinned daemon identity")
			}
			return found, nil
		}
		if found.ConnectionState == "auth_required" || found.ConnectionState == "incompatible" || found.ReasonCode == "RECONNECT_EXHAUSTED" {
			return nil, fmt.Errorf("%s: %s; %s", safeText(found.ReasonCode), client.ConnectionRecovery(found), client.ConnectionAttemptsNotice(found))
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
		}
	}
}

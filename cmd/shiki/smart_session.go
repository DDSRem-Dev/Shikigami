package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"

	"github.com/DDSRem-Dev/Shikigami/internal/client"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
)

type smartSessionChoice struct{ action, label string }

// One smart invocation can perform several independent mutations. A supplied
// batch request ID must not collide across these operations or conflict files.
func (a *app) smartRequestID(operation string) string {
	hash := sha256.Sum256([]byte(a.id() + "\x00" + operation))
	return "smart_" + hex.EncodeToString(hash[:])
}

func smartGrant(agreement *pb.ProtocolAgreement, permission string) bool {
	return agreement.GetDaemonId() != "" && slices.Contains(agreement.GrantedPermissions, permission)
}

func activeSessionChoices(session *pb.Session, agreement *pb.ProtocolAgreement) []smartSessionChoice {
	var choices []smartSessionChoice
	if agreement.GetDaemonId() == "" || smartGrant(agreement, "session.attach") {
		choices = append(choices, smartSessionChoice{"observe", fmt.Sprintf("Observe %s (read-only; project %s)", session.Name, session.ProjectId)})
	}
	if smartGrant(agreement, "session.create") {
		choices = append(choices, smartSessionChoice{"create", "Create a new session in this project"})
	}
	return append(choices, smartSessionChoice{"manage", "Manage sessions in the dashboard"}, smartSessionChoice{"exit", "Exit (existing sessions keep running)"})
}

func (a *app) createSmartSession(ctx context.Context, c *client.Client, server, project string, agreement *pb.ProtocolAgreement) (*pb.Session, error) {
	if !smartGrant(agreement, "session.create") {
		return nil, errors.New("session.create permission is unavailable; use an authorized credential to create a session")
	}
	name, err := a.prompt(ctx, "New session name", "")
	if err != nil {
		return nil, err
	}
	if name == "" {
		return nil, errors.New("session name is required")
	}
	workingDir, err := a.chooseWorkingDirectory(ctx, c, server, project, agreement)
	if err != nil {
		return nil, err
	}
	description, err := a.prompt(ctx, "Description (optional)", "")
	if err != nil {
		return nil, err
	}
	created, err := c.Remote(server).CreateSession(ctx, &pb.CreateSessionRequest{ProjectId: project, Name: name, WorkingDir: workingDir, Description: description, RequestId: a.smartRequestID("session:" + server + ":" + project + ":" + name)})
	if err != nil {
		return nil, err
	}
	fmt.Fprintf(a.errorOutput, "Created session %s · server %s · project %s · %s\n", safeText(created.Name), safeText(server), safeText(project), safeText(created.WorkingDir))
	return created, nil
}

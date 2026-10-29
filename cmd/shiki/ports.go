package main

import (
	"errors"
	"fmt"
	"net"
	"strconv"
	"text/tabwriter"

	"github.com/DDSRem-Dev/Shikigami/internal/client"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	apb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent"
	ppb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin"
	"github.com/spf13/cobra"
)

func (a *app) portCommands() *cobra.Command {
	root := &cobra.Command{Use: "ports", Short: "Manage local loopback forwards"}
	list := &cobra.Command{Use: "list", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		return a.withClient(cmd.Context(), true, func(c *client.Client) error {
			result, err := c.Agent().ListForwards(cmd.Context(), &pb.Empty{})
			if err != nil {
				return err
			}
			if a.server != "" {
				server, err := c.ResolveServer(cmd.Context(), a.server)
				if err != nil {
					return err
				}
				filtered := result.Forwards[:0]
				for _, state := range result.Forwards {
					if state.Spec.ServerId == server.Config.Id {
						filtered = append(filtered, state)
					}
				}
				result.Forwards = filtered
			}
			return a.printForwards(result)
		})
	}}
	root.RunE = list.RunE
	root.Args = cobra.NoArgs
	root.AddCommand(list)
	var localPort uint32
	var bind, remote, session string
	forward := &cobra.Command{Use: "forward <remote-port>", Args: cobra.ExactArgs(1), Short: "Create a manual forward allowed by the remote port plugin", RunE: func(cmd *cobra.Command, args []string) error {
		port, err := strconv.ParseUint(args[0], 10, 16)
		if err != nil || port == 0 {
			return errors.New("remote port must be between 1 and 65535")
		}
		if localPort > 65535 {
			return errors.New("local port must be between 0 and 65535")
		}
		chosen := localPort
		if !cmd.Flags().Changed("local-port") {
			chosen = uint32(port)
		}
		return a.withClient(cmd.Context(), true, func(c *client.Client) error {
			server, project, err := a.selected(cmd.Context(), c)
			if err != nil {
				return err
			}
			sessionID := ""
			if session != "" {
				record, err := c.ResolveSession(cmd.Context(), server, project, session)
				if err != nil {
					return err
				}
				sessionID = record.Id
			}
			state, err := c.Agent().CreateForward(cmd.Context(), &apb.CreateForwardRequest{Spec: &ppb.ForwardSpec{ServerId: server, SessionId: sessionID, RemoteAddress: remote, RemotePort: uint32(port), LocalAddress: bind, LocalPort: chosen, Enabled: true}, RequestId: a.id()})
			if err != nil {
				return err
			}
			if a.json {
				err = a.print(state)
			} else {
				err = a.printForwards(&ppb.ForwardList{Forwards: []*ppb.ForwardState{state}})
			}
			if err != nil {
				return err
			}
			if state.State != "listening" || state.BoundPort == 0 {
				return fmt.Errorf("forward %s is %s: %s", state.Spec.Id, state.State, state.ReasonCode)
			}
			return nil
		})
	}}
	forward.Flags().Uint32Var(&localPort, "local-port", 0, "Local port (defaults to the remote port; explicit 0 lets the OS choose)")
	forward.Flags().StringVar(&bind, "bind-address", "", "Literal local loopback address (defaults to port plugin configuration)")
	forward.Flags().StringVar(&remote, "remote-address", "127.0.0.1", "Literal remote loopback address")
	forward.Flags().StringVar(&session, "session", "", "Optional remote session ID or unambiguous name")
	root.AddCommand(forward)
	var revision string
	stop := &cobra.Command{Use: "stop <forward-id>", Args: cobra.ExactArgs(1), Short: "Stop connections and suppress automatic recreation for the same process", RunE: func(cmd *cobra.Command, args []string) error {
		return a.withClient(cmd.Context(), true, func(c *client.Client) error {
			expected := revision
			if expected == "" {
				list, err := c.Agent().ListForwards(cmd.Context(), &pb.Empty{})
				if err != nil {
					return err
				}
				for _, state := range list.Forwards {
					if state.Spec.Id == args[0] {
						expected = state.Spec.Revision
						break
					}
				}
				if expected == "" {
					return errors.New("forward not found; an intentional request retry also requires its original --revision")
				}
			}
			result, err := c.Agent().StopForward(cmd.Context(), &apb.StopForwardRequest{ForwardId: args[0], ExpectedRevision: expected, RequestId: a.id()})
			if err != nil {
				return err
			}
			return a.print(result)
		})
	}}
	stop.Flags().StringVar(&revision, "revision", "", "Expected revision; retain it with --request-id for an identical retry")
	root.AddCommand(stop)
	return root
}
func (a *app) printForwards(list *ppb.ForwardList) error {
	if a.json {
		return a.print(list)
	}
	table := tabwriter.NewWriter(a.output, 0, 4, 2, ' ', 0)
	fmt.Fprintln(table, "ID\tSERVER\tMODE\tREMOTE\tLOCAL\tSTATE\tCONNECTIONS\tBYTES\tREVISION\tREASON")
	for _, state := range list.Forwards {
		spec := state.Spec
		if spec == nil {
			continue
		}
		local := "—"
		if state.BoundPort != 0 {
			local = net.JoinHostPort(spec.LocalAddress, strconv.Itoa(int(state.BoundPort)))
		}
		mode := "manual"
		if spec.Automatic {
			mode = "auto"
		}
		fmt.Fprintf(table, "%s\t%s\t%s\t%s\t%s\t%s\t%d\t%d\t%s\t%s\n", safeText(spec.Id), safeText(spec.ServerId), mode, net.JoinHostPort(spec.RemoteAddress, strconv.Itoa(int(spec.RemotePort))), local, safeText(state.State), state.Connections, state.BytesTransferred, spec.Revision, safeText(state.ReasonCode))
	}
	return table.Flush()
}

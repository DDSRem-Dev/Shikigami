package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/DDSRem-Dev/Shikigami/internal/client"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	apb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent"
	"github.com/anmitsu/go-shlex"
	"github.com/spf13/cobra"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// Configuration edits operate on typed IPC snapshots, never on agent files.
func (a *app) configCommands() *cobra.Command {
	root := &cobra.Command{Use: "config", Short: "Read or update preferences (or a server with --server) through the agent"}
	root.AddCommand(&cobra.Command{Use: "startup", Args: cobra.NoArgs, Short: "Inspect CLI and managed login/boot startup policy without changing services", RunE: func(cmd *cobra.Command, _ []string) error {
		if a.server != "" || a.project != "" {
			return errors.New("config startup is local; --server and --project do not apply")
		}
		return a.withClient(cmd.Context(), false, func(c *client.Client) error {
			value, err := c.StartupPolicy(cmd.Context())
			if err != nil {
				return err
			}
			if a.json {
				return a.print(value)
			}
			fmt.Fprintf(a.output, "Configuration loaded by agent: %s\nCLI on-demand start (agent-loaded configuration): %s (explicit agent start remains available)\nManaged service: %s (%s)\nLogin start: %s\nBoot start: %s\nReason: %s\nOnly the standard Shiki service is checked; other login items and custom automation are not included.\n", safeText(value.ConfigPath), safeText(value.CliAutoStart), safeText(value.ManagedService), safeText(value.Manager), safeText(value.LoginStart), safeText(value.BootStart), safeText(value.ReasonCode))
			return nil
		})
	}})
	root.AddCommand(&cobra.Command{Use: "get", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		return a.withClient(cmd.Context(), true, func(c *client.Client) error {
			value, err := a.readConfiguration(cmd.Context(), c)
			if err != nil {
				return err
			}
			return a.print(value)
		})
	}})
	root.AddCommand(&cobra.Command{Use: "set <field> <value>", Args: cobra.ExactArgs(2), Short: "Set one preference or server field with revision protection", RunE: func(cmd *cobra.Command, args []string) error {
		return a.withClient(cmd.Context(), true, func(c *client.Client) error {
			before, err := a.readConfiguration(cmd.Context(), c)
			if err != nil {
				return err
			}
			after := proto.Clone(before)
			if err = setConfigurationField(after, args[0], args[1]); err != nil {
				return err
			}
			return a.saveConfiguration(cmd.Context(), c, before, after)
		})
	}})
	var file string
	edit := &cobra.Command{Use: "edit", Args: cobra.NoArgs, Short: "Edit a private JSON snapshot using VISUAL/EDITOR, or apply --file", RunE: func(cmd *cobra.Command, _ []string) error {
		if file == "" && !a.interactive() {
			return errors.New("config edit requires a terminal or explicit --file")
		}
		return a.withClient(cmd.Context(), true, func(c *client.Client) error {
			before, err := a.readConfiguration(cmd.Context(), c)
			if err != nil {
				return err
			}
			var data []byte
			if file != "" {
				data, err = readConfigurationEdit(file)
			} else {
				data, err = a.editConfiguration(cmd.Context(), before)
			}
			if err != nil {
				return err
			}
			after := before.ProtoReflect().Type().New().Interface()
			// Default protojson rejects unknown fields, duplicate fields and trailing
			// documents. Credentials have no field in either readable message.
			if err = protojson.Unmarshal(data, after); err != nil {
				return errors.New("invalid configuration JSON: unknown, duplicate, or incorrectly typed field")
			}
			return a.saveConfiguration(cmd.Context(), c, before, after)
		})
	}}
	edit.Flags().StringVar(&file, "file", "", "Apply an owned 0600 JSON snapshot, preserving its revision")
	root.AddCommand(edit)
	return root
}

func (a *app) readConfiguration(ctx context.Context, c *client.Client) (proto.Message, error) {
	if a.project != "" {
		return nil, errors.New("config does not accept --project; use project or mapping commands")
	}
	if a.server != "" {
		return c.ResolveServer(ctx, a.server)
	}
	return c.Agent().GetPreferences(ctx, &pb.Empty{})
}

func setConfigurationField(snapshot proto.Message, name, value string) error {
	var target proto.Message = snapshot
	if record, ok := snapshot.(*apb.ServerRecord); ok {
		target = record.Config
	}
	if target == nil {
		return errors.New("configuration is missing")
	}
	message := target.ProtoReflect()
	field := message.Descriptor().Fields().ByName(protoreflect.Name(name))
	if field == nil || name == "revision" || name == "id" {
		return fmt.Errorf("unknown or read-only configuration field %q", safeText(name))
	}
	switch field.Kind() {
	case protoreflect.StringKind:
		message.Set(field, protoreflect.ValueOfString(value))
	case protoreflect.BoolKind:
		if value != "true" && value != "false" {
			return errors.New("boolean configuration values must be true or false")
		}
		parsed, _ := strconv.ParseBool(value)
		message.Set(field, protoreflect.ValueOfBool(parsed))
	default:
		return errors.New("configuration field cannot be set as a scalar")
	}
	return nil
}

func validateConfigurationEdit(before, after proto.Message) error {
	switch old := before.(type) {
	case *apb.Preferences:
		next, ok := after.(*apb.Preferences)
		if !ok || next.Revision != old.Revision {
			return errors.New("configuration revision changed; reload before editing")
		}
	case *apb.ServerRecord:
		next, ok := after.(*apb.ServerRecord)
		if !ok || old.Config == nil || next.Config == nil || next.Config.Id != old.Config.Id {
			return errors.New("server identity cannot be edited")
		}
		oldMeta, nextMeta := proto.Clone(old).(*apb.ServerRecord), proto.Clone(next).(*apb.ServerRecord)
		oldMeta.Config, nextMeta.Config = nil, nil
		if !proto.Equal(oldMeta, nextMeta) {
			return errors.New("server revision, credential indicators and runtime status are read-only")
		}
	default:
		return errors.New("unsupported configuration scope")
	}
	return nil
}

func (a *app) saveConfiguration(ctx context.Context, c *client.Client, before, after proto.Message) error {
	if err := validateConfigurationEdit(before, after); err != nil {
		return err
	}
	if proto.Equal(before, after) {
		return a.print(before)
	}
	var saved proto.Message
	var err error
	switch next := after.(type) {
	case *apb.Preferences:
		saved, err = c.Agent().SetPreferences(ctx, &apb.SetPreferencesRequest{Values: next, RequestId: a.id()})
	case *apb.ServerRecord:
		saved, err = c.Agent().UpsertServer(ctx, &apb.UpsertServerRequest{Config: next.Config, ExpectedRevision: next.Revision, RequestId: a.id()})
	}
	if err != nil {
		return err
	}
	return a.print(saved)
}

func readConfigurationEdit(path string) ([]byte, error) {
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), path)
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || !ok || stat.Uid != uint32(os.Getuid()) {
		return nil, errors.New("configuration edit must be an owned regular 0600 file")
	}
	if info.Size() == 0 || info.Size() > 1<<20 {
		return nil, errors.New("configuration edit is empty or exceeds 1 MiB")
	}
	data, err := io.ReadAll(io.LimitReader(f, (1<<20)+1))
	if err != nil {
		return nil, err
	}
	if len(data) == 0 || len(data) > 1<<20 {
		return nil, errors.New("configuration edit is empty or exceeds 1 MiB")
	}
	return data, nil
}

func (a *app) editConfiguration(ctx context.Context, value proto.Message) ([]byte, error) {
	editor := os.Getenv("VISUAL")
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	if editor == "" {
		editor = "vi"
	}
	argv, err := shlex.Split(editor, true)
	if err != nil || len(argv) == 0 {
		return nil, errors.New("invalid VISUAL/EDITOR command")
	}
	dir, err := os.MkdirTemp("", "shiki-config-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "config.json")
	data, err := protojson.MarshalOptions{UseProtoNames: true, EmitUnpopulated: true, Indent: "  "}.Marshal(value)
	if err != nil {
		return nil, err
	}
	if err = os.WriteFile(path, append(data, '\n'), 0600); err != nil {
		return nil, err
	}
	command := exec.CommandContext(ctx, argv[0], append(argv[1:], path)...)
	command.Stdin, command.Stdout, command.Stderr = a.input, a.output, a.errorOutput
	command.WaitDelay = time.Second
	if err = command.Run(); err != nil {
		return nil, fmt.Errorf("configuration editor failed: %w", err)
	}
	return readConfigurationEdit(path)
}

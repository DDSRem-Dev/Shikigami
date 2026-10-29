package install

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/DDSRem-Dev/Shikigami/internal/strictjson"
	"io"
	"os"
	"os/user"
	"path/filepath"

	"github.com/DDSRem-Dev/Shikigami/pkg/config"
	"github.com/DDSRem-Dev/Shikigami/pkg/plugin/buildinfo"
)

type UserOperation struct {
	Prefix          string `json:"prefix"`
	Plan            Plan   `json:"plan"`
	Key             []byte `json:"key,omitempty"`
	Interactive     bool   `json:"interactive,omitempty"`
	ExpectedVersion string `json:"expected_version"`
}

// UserHelper is invoked with OS credentials already dropped. Its input travels
// over an anonymous pipe; authentication material never appears in argv/env.
func UserHelper(ctx context.Context, operation string, in io.Reader, out io.Writer) error {
	var request UserOperation
	data, err := io.ReadAll(io.LimitReader(in, (64<<10)+1))
	if err != nil {
		return err
	}
	defer clear(data)
	if len(data) > 64<<10 {
		return errors.New("private installer request exceeds limit")
	}
	if err := strictjson.Decode(data, &request); err != nil {
		return err
	}
	defer clear(request.Key)
	if request.ExpectedVersion == "" || request.ExpectedVersion != buildinfo.Version {
		return errors.New("helper build differs from the selected installation release")
	}
	account, err := user.Current()
	if err != nil {
		return err
	}
	if account.Uid == "0" || account.Username != request.Plan.User {
		return errors.New("bootstrap must run as the configured ordinary user")
	}
	switch operation {
	case "config":
		if err := initializeConfig(request.Plan); err != nil {
			return err
		}
		return installUserLinks(request.Plan, request.Prefix)
	case "unlink":
		return removeUserLinks(request.Plan, request.Prefix)
	case "probe":
		status, err := probeUser(ctx, request)
		if err != nil {
			return err
		}
		return json.NewEncoder(out).Encode(status)
	default:
		return errors.New("unknown private installer operation")
	}
}
func initializeConfig(plan Plan) error {
	if plan.Role == "client" {
		existing, err := config.LoadClient(plan.Config)
		if err == nil {
			if existing.Agent.StateDir != plan.StateDir || existing.PluginsDir != plan.PluginsDir || existing.Tailscale.StateDir != plan.TailscaleDir {
				return errors.New("existing client configuration differs from installation paths")
			}
			return nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		cfg := config.DefaultClient()
		cfg.Agent.StateDir = plan.StateDir
		cfg.PluginsDir = plan.PluginsDir
		cfg.Agent.Socket = filepath.Join(filepath.Dir(plan.Config), "run", "agent.sock")
		cfg.Tailscale.StateDir = plan.TailscaleDir
		return config.SaveClient(plan.Config, cfg)
	}
	existing, err := config.LoadDaemon(plan.Config)
	if err == nil {
		if existing.RunUser != plan.User || existing.StateDir != plan.StateDir || existing.PluginsDir != plan.PluginsDir || existing.Tailscale.StateDir != plan.TailscaleDir || existing.Server.Port != plan.Port || existing.ProjectsDir != plan.ProjectsDir || existing.Claude.Executable != plan.Claude {
			return errors.New("existing daemon configuration differs; nothing was overwritten")
		}
		return nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	cfg := config.DefaultDaemon(plan.User, plan.StateDir)
	cfg.PluginsDir = plan.PluginsDir
	cfg.Tailscale.StateDir = plan.TailscaleDir
	cfg.ProjectsDir = plan.ProjectsDir
	cfg.Server.Port = plan.Port
	cfg.Claude.Executable = plan.Claude
	cfg.Server.DirectTLS = config.DirectTLS{Enabled: plan.TLSAddress != "", Address: plan.TLSAddress, CertFile: plan.TLSCert, KeyFile: plan.TLSKey}
	return config.SaveDaemon(plan.Config, cfg)
}

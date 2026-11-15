package runtime

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net"
	"path/filepath"
	"regexp"
	"strings"

	epb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/extensions"
)

const Binary = "/usr/lib/shikigami/shiki-browser-runtime"
const ChromeBinary = "/usr/bin/google-chrome-stable"
const MaxControlBody = 64 << 10
const MaxWireFrame = 64 << 10

var executableHashPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
var identifier = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$`)

func ValidID(value string) bool { return identifier.MatchString(value) }

func UTF8Locale(value string) bool {
	base, _, _ := strings.Cut(value, "@")
	return (len(base) >= 5 && strings.EqualFold(base[len(base)-5:], ".utf8")) || (len(base) >= 6 && strings.EqualFold(base[len(base)-6:], ".utf-8"))
}

func NewID() string {
	var data [16]byte
	if _, err := rand.Read(data[:]); err != nil {
		panic("browser identity entropy unavailable")
	}
	return hex.EncodeToString(data[:])
}

// Spec is an immutable, private launch record, not a PID-based process lease.
// It outlives the plugin instance and its callback credential.
type Spec struct {
	ProfileID        string            `json:"profile_id"`
	ProfileRevision  string            `json:"profile_revision"`
	InstanceID       string            `json:"instance_id"`
	Token            string            `json:"token"`
	Unit             string            `json:"unit"`
	ConfigPath       string            `json:"config_path"`
	SocketDir        string            `json:"socket_dir"`
	ProfileDir       string            `json:"profile_dir"`
	ExecutableSHA256 string            `json:"executable_sha256"`
	Region           *epb.RegionConfig `json:"region,omitempty"`
	RegionRevision   string            `json:"region_revision"`
	Width            int               `json:"width"`
	Height           int               `json:"height"`
}

type Snapshot struct {
	ProfileID      string   `json:"profile_id"`
	InstanceID     string   `json:"instance_id"`
	State          string   `json:"state"`
	Reason         string   `json:"reason,omitempty"`
	ChromeVersion  string   `json:"chrome_version,omitempty"`
	RegionRevision string   `json:"region_revision"`
	Warnings       []string `json:"warnings,omitempty"`
}

type Manager interface {
	Prepare(context.Context, string, string, string, string, *epb.RegionConfig, string, int, int) (Spec, error)
	Launch(context.Context, Spec) (Snapshot, error)
	Inspect(context.Context, Spec) (Snapshot, error)
	Stop(context.Context, Spec) error
	Dial(context.Context, Spec) (net.Conn, error)
	Diagnose(context.Context, Spec, *epb.DiagnosticRequest) (*epb.DiagnosticReport, error)
}

func (s Spec) Validate() error {
	if !ValidID(s.ProfileID) || !ValidID(s.InstanceID) || !ValidID(s.Token) || !ValidID(s.Unit) || !executableHashPattern.MatchString(s.ExecutableSHA256) || !filepath.IsAbs(s.ConfigPath) || !filepath.IsAbs(s.SocketDir) || !filepath.IsAbs(s.ProfileDir) || s.Width < 640 || s.Width > 4096 || s.Height < 480 || s.Height > 2160 {
		return errors.New("invalid private browser runtime specification")
	}
	if len(filepath.Join(s.SocketDir, "control.sock")) > 100 {
		return errors.New("browser runtime socket path too long")
	}
	return nil
}

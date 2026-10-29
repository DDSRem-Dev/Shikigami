package version

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connectproto"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/protobuf/proto"
)

const diagnosticDomain = "shiki.protocol"

var safeBuild = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]{0,63}$`)
var safeCapability = regexp.MustCompile(`^[a-z][a-z0-9._-]{0,95}$`)

func incompatible(local, peer *pb.ProtocolOffer, build string, missing []string) *connect.Error {
	metadata := map[string]string{
		"local_major": strconv.FormatUint(uint64(local.GetMajor()), 10), "local_min_minor": strconv.FormatUint(uint64(local.GetMinMinor()), 10), "local_max_minor": strconv.FormatUint(uint64(local.GetMaxMinor()), 10),
		"peer_major": strconv.FormatUint(uint64(peer.GetMajor()), 10), "peer_min_minor": strconv.FormatUint(uint64(peer.GetMinMinor()), 10), "peer_max_minor": strconv.FormatUint(uint64(peer.GetMaxMinor()), 10),
	}
	if safeBuild.MatchString(build) {
		metadata["local_build"] = build
	}
	var names []string
	seen := map[string]bool{}
	for _, name := range missing {
		if safeCapability.MatchString(name) && !seen[name] {
			seen[name] = true
			names = append(names, name)
		}
	}
	sort.Strings(names)
	if len(names) > 8 {
		names = names[:8]
	}
	if len(names) > 0 {
		metadata["missing_capabilities"] = strings.Join(names, ",")
	}
	detail, _ := connectproto.NewErrorDetail(&errdetails.ErrorInfo{Reason: "PROTOCOL_INCOMPATIBLE", Domain: diagnosticDomain, Metadata: metadata})
	return connect.NewError(connect.CodeFailedPrecondition, "PROTOCOL_INCOMPATIBLE").WithDetail(detail)
}

// SafeDetail admits only one bounded, typed protocol diagnostic. Arbitrary
// downstream messages, metadata, debug JSON and unrelated details never pass.
func SafeDetail(err error) *connect.ErrorDetail {
	var failure *connect.Error
	if !errors.As(err, &failure) || failure.Code() != connect.CodeFailedPrecondition {
		return nil
	}
	for index, detail := range failure.Details() {
		if index >= 8 {
			break
		}
		if detail == nil || detail.Type != "google.rpc.ErrorInfo" || len(detail.Value) > 2048 {
			continue
		}
		var info errdetails.ErrorInfo
		if proto.Unmarshal(detail.Value, &info) != nil || info.Domain != diagnosticDomain {
			continue
		}
		if info.Reason == "REMOTE_CAPABILITY_UNAVAILABLE" {
			if len(info.Metadata) != 2 || info.Metadata["component"] != "daemon" || !safeCapability.MatchString(info.Metadata["capability"]) {
				continue
			}
			result, _ := connectproto.NewErrorDetail(&errdetails.ErrorInfo{Reason: info.Reason, Domain: diagnosticDomain, Metadata: map[string]string{"component": "daemon", "capability": info.Metadata["capability"]}})
			return result
		}
		if info.Reason != "PROTOCOL_INCOMPATIBLE" || len(info.Metadata) > 9 {
			continue
		}
		metadata := map[string]string{}
		valid := true
		for _, key := range []string{"local_major", "local_min_minor", "local_max_minor", "peer_major", "peer_min_minor", "peer_max_minor"} {
			value, ok := info.Metadata[key]
			if !ok || len(value) > 10 {
				valid = false
				break
			}
			parsed, err := strconv.ParseUint(value, 10, 32)
			if err != nil || strconv.FormatUint(parsed, 10) != value {
				valid = false
				break
			}
			metadata[key] = value
		}
		for key, value := range info.Metadata {
			switch key {
			case "local_major", "local_min_minor", "local_max_minor", "peer_major", "peer_min_minor", "peer_max_minor":
			case "local_build":
				if !safeBuild.MatchString(value) {
					valid = false
				} else {
					metadata[key] = value
				}
			case "component":
				if value != "agent" && value != "daemon" && value != "plugin" && value != "node" {
					valid = false
				} else {
					metadata[key] = value
				}
			case "missing_capabilities":
				parts := strings.Split(value, ",")
				if len(parts) > 8 {
					valid = false
				}
				for _, name := range parts {
					if !safeCapability.MatchString(name) {
						valid = false
					}
				}
				metadata[key] = value
			default:
				valid = false
			}
		}
		if valid {
			result, _ := connectproto.NewErrorDetail(&errdetails.ErrorInfo{Reason: info.Reason, Domain: diagnosticDomain, Metadata: metadata})
			return result
		}
	}
	return nil
}

func WithComponent(err error, component string) error {
	detail := SafeDetail(err)
	if detail == nil {
		return err
	}
	var info errdetails.ErrorInfo
	_ = proto.Unmarshal(detail.Value, &info)
	info.Metadata["component"] = component
	encoded, _ := connectproto.NewErrorDetail(&info)
	result := connect.NewError(connect.CodeFailedPrecondition, info.Reason).WithDetail(encoded).WithCause(err)
	if SafeDetail(result) == nil {
		return err
	}
	return result
}

// Describe is used only at the final CLI display boundary. All interpolated
// peer strings came through SafeDetail rather than an untrusted error message.
func Describe(err error, clientBuild string) string {
	detail := SafeDetail(err)
	if detail == nil {
		return err.Error()
	}
	var info errdetails.ErrorInfo
	_ = proto.Unmarshal(detail.Value, &info)
	m := info.Metadata
	if info.Reason == "REMOTE_CAPABILITY_UNAVAILABLE" {
		return "REMOTE_CAPABILITY_UNAVAILABLE: selected server's daemon did not negotiate " + m["capability"] + "; use a daemon version that supports this capability"
	}
	component := m["component"]
	if component == "" {
		component = "peer"
	}
	build := m["local_build"]
	if build == "" {
		build = "unknown"
	}
	if !safeBuild.MatchString(clientBuild) {
		clientBuild = "unknown"
	}
	message := fmt.Sprintf("PROTOCOL_INCOMPATIBLE: %s build %s accepts major %s minor %s..%s; requester offered major %s minor %s..%s; this CLI build %s", component, build, m["local_major"], m["local_min_minor"], m["local_max_minor"], m["peer_major"], m["peer_min_minor"], m["peer_max_minor"], clientBuild)
	if missing := m["missing_capabilities"]; missing != "" {
		message += "; missing required capability: " + missing
	}
	return message + "; install compatible versions of the " + component + " and its client"
}

// MissingRemoteCapability describes local admission failure without exposing
// server addresses, credentials, or peer-controlled diagnostic text.
func MissingRemoteCapability(capability string) error {
	err := connect.NewError(connect.CodeFailedPrecondition, "REMOTE_CAPABILITY_UNAVAILABLE")
	if !safeCapability.MatchString(capability) {
		return err
	}
	detail, _ := connectproto.NewErrorDetail(&errdetails.ErrorInfo{Reason: "REMOTE_CAPABILITY_UNAVAILABLE", Domain: diagnosticDomain, Metadata: map[string]string{"component": "daemon", "capability": capability}})
	return err.WithDetail(detail)
}

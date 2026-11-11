// Package plugin — ActionRequest/ActionResult are the typed Go wrappers for
// the InvokeAction RPC which travels over gRPC as google.protobuf.Struct.
// pkg/protocol/plugin/invoke_action.go carries the gRPC wire binding.
package plugin

import (
	"context"
	"encoding/json"
	"fmt"

	ppb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin"
	structpb "google.golang.org/protobuf/types/known/structpb"
)

// ActionResultKind discriminates the oneof result field in ActionResult.
type ActionResultKind int

const (
	// ActionResultText carries a plain-text string for CLI output.
	ActionResultText ActionResultKind = iota
	// ActionResultForm carries a structured form for TUI/GUI rendering.
	ActionResultForm
	// ActionResultJSON carries a JSON object for --json CLI output.
	ActionResultJSON
	// ActionResultNotify carries an in-app notification.
	ActionResultNotify
)

// ActionRequest is the typed input to an InvokeAction call.
type ActionRequest struct {
	// ActionID identifies the manifest-declared action (e.g. "list-conflicts").
	ActionID string `json:"action_id"`
	// InvocationID is a unique per-call identifier for deduplication.
	InvocationID string `json:"invocation_id"`
	// Scope limits the action to a specific project/session/mapping context.
	Scope *ppb.Scope `json:"scope,omitempty"`
	// Flags carries the parsed CLI flags (flag name → string value).
	Flags map[string]string `json:"flags,omitempty"`
	// InputJSON carries form submission data as a JSON string.
	InputJSON string `json:"input_json,omitempty"`
}

// ActionResult is the typed output of an InvokeAction call.
type ActionResult struct {
	// Kind selects which field below is populated.
	Kind ActionResultKind
	// Text is the plain-text CLI output (Kind == ActionResultText).
	Text string
	// Form is a structured form descriptor for TUI/GUI display (Kind == ActionResultForm).
	Form *ActionForm
	// JSON is the parsed JSON output for --json mode (Kind == ActionResultJSON).
	JSON map[string]any
	// Notify is an in-app notification descriptor (Kind == ActionResultNotify).
	Notify *NotifyPayload
	// Done signals this is the final result of the invocation.
	Done bool
}

// ActionForm describes an interactive form returned by an action handler.
type ActionForm struct {
	Title          string
	Description    string
	Fields         []ConfigField
	SubmitActionID string
	CancelActionID string
}

// ActionImplementation is an optional interface that plugin implementations
// may satisfy to handle manifest-declared actions. If the Implementation
// does not implement ActionImplementation, InvokeAction returns Unimplemented.
type ActionImplementation interface {
	InvokeAction(ctx context.Context, req *ActionRequest) (*ActionResult, error)
}

// MarshalActionRequest converts ActionRequest to a structpb.Struct for gRPC transport.
func MarshalActionRequest(req *ActionRequest) (*structpb.Struct, error) {
	if req == nil {
		return nil, fmt.Errorf("action request is nil")
	}
	m := map[string]any{
		"action_id":     req.ActionID,
		"invocation_id": req.InvocationID,
	}
	if req.Flags != nil {
		flags := map[string]any{}
		for k, v := range req.Flags {
			flags[k] = v
		}
		m["flags"] = flags
	}
	if req.InputJSON != "" {
		m["input_json"] = req.InputJSON
	}
	if req.Scope != nil {
		scope := map[string]any{}
		if req.Scope.ServerId != "" {
			scope["server_id"] = req.Scope.ServerId
		}
		if req.Scope.ProjectId != "" {
			scope["project_id"] = req.Scope.ProjectId
		}
		if req.Scope.SessionId != "" {
			scope["session_id"] = req.Scope.SessionId
		}
		if req.Scope.MappingId != "" {
			scope["mapping_id"] = req.Scope.MappingId
		}
		if len(scope) > 0 {
			m["scope"] = scope
		}
	}
	return structpb.NewStruct(m)
}

// UnmarshalActionRequest reconstructs ActionRequest from a structpb.Struct.
func UnmarshalActionRequest(s *structpb.Struct) (*ActionRequest, error) {
	if s == nil {
		return nil, fmt.Errorf("action request struct is nil")
	}
	fields := s.GetFields()
	req := &ActionRequest{}
	if v, ok := fields["action_id"]; ok {
		req.ActionID = v.GetStringValue()
	}
	if v, ok := fields["invocation_id"]; ok {
		req.InvocationID = v.GetStringValue()
	}
	if req.ActionID == "" {
		return nil, fmt.Errorf("action_id is required")
	}
	if v, ok := fields["flags"]; ok {
		if sv := v.GetStructValue(); sv != nil {
			req.Flags = make(map[string]string, len(sv.GetFields()))
			for k, fv := range sv.GetFields() {
				req.Flags[k] = fv.GetStringValue()
			}
		}
	}
	if v, ok := fields["input_json"]; ok {
		req.InputJSON = v.GetStringValue()
	}
	if v, ok := fields["scope"]; ok {
		if sv := v.GetStructValue(); sv != nil {
			sf := sv.GetFields()
			req.Scope = &ppb.Scope{}
			if v2, ok := sf["server_id"]; ok {
				req.Scope.ServerId = v2.GetStringValue()
			}
			if v2, ok := sf["project_id"]; ok {
				req.Scope.ProjectId = v2.GetStringValue()
			}
			if v2, ok := sf["session_id"]; ok {
				req.Scope.SessionId = v2.GetStringValue()
			}
			if v2, ok := sf["mapping_id"]; ok {
				req.Scope.MappingId = v2.GetStringValue()
			}
		}
	}
	return req, nil
}

// MarshalActionResult converts ActionResult to a structpb.Struct for gRPC transport.
func MarshalActionResult(res *ActionResult) (*structpb.Struct, error) {
	if res == nil {
		return nil, fmt.Errorf("action result is nil")
	}
	m := map[string]any{"done": res.Done}
	switch res.Kind {
	case ActionResultText:
		m["kind"] = "text"
		m["text"] = res.Text
	case ActionResultForm:
		m["kind"] = "form"
		if res.Form != nil {
			fb, err := json.Marshal(res.Form)
			if err != nil {
				return nil, fmt.Errorf("marshal form: %w", err)
			}
			var form map[string]any
			if err = json.Unmarshal(fb, &form); err != nil {
				return nil, fmt.Errorf("re-unmarshal form: %w", err)
			}
			m["form"] = form
		}
	case ActionResultJSON:
		m["kind"] = "json"
		if res.JSON != nil {
			m["json"] = res.JSON
		}
	case ActionResultNotify:
		m["kind"] = "notification"
		if res.Notify != nil {
			nb, err := json.Marshal(res.Notify)
			if err != nil {
				return nil, fmt.Errorf("marshal notification: %w", err)
			}
			var notify map[string]any
			if err = json.Unmarshal(nb, &notify); err != nil {
				return nil, fmt.Errorf("re-unmarshal notification: %w", err)
			}
			m["notify"] = notify
		}
	}
	return structpb.NewStruct(m)
}

// UnmarshalActionResult reconstructs ActionResult from a structpb.Struct.
func UnmarshalActionResult(s *structpb.Struct) (*ActionResult, error) {
	if s == nil {
		return nil, fmt.Errorf("action result struct is nil")
	}
	fields := s.GetFields()
	res := &ActionResult{}
	if v, ok := fields["done"]; ok {
		res.Done = v.GetBoolValue()
	}
	kind := ""
	if v, ok := fields["kind"]; ok {
		kind = v.GetStringValue()
	}
	switch kind {
	case "text", "":
		res.Kind = ActionResultText
		if v, ok := fields["text"]; ok {
			res.Text = v.GetStringValue()
		}
	case "form":
		res.Kind = ActionResultForm
		if v, ok := fields["form"]; ok {
			if sv := v.GetStructValue(); sv != nil {
				b, err := json.Marshal(sv.AsMap())
				if err != nil {
					return nil, fmt.Errorf("marshal form struct: %w", err)
				}
				form := &ActionForm{}
				if err = json.Unmarshal(b, form); err != nil {
					return nil, fmt.Errorf("unmarshal form: %w", err)
				}
				res.Form = form
			}
		}
	case "json":
		res.Kind = ActionResultJSON
		if v, ok := fields["json"]; ok {
			if sv := v.GetStructValue(); sv != nil {
				res.JSON = sv.AsMap()
			}
		}
	case "notification":
		res.Kind = ActionResultNotify
		if v, ok := fields["notify"]; ok {
			if sv := v.GetStructValue(); sv != nil {
				b, err := json.Marshal(sv.AsMap())
				if err != nil {
					return nil, fmt.Errorf("marshal notify struct: %w", err)
				}
				notify := &NotifyPayload{}
				if err = json.Unmarshal(b, notify); err != nil {
					return nil, fmt.Errorf("unmarshal notification: %w", err)
				}
				res.Notify = notify
			}
		}
	default:
		return nil, fmt.Errorf("unknown action result kind %q", kind)
	}
	return res, nil
}

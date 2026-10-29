package portplugin

import (
	"google.golang.org/protobuf/types/known/structpb"
	"testing"
)

func TestExplicitAllowlistLoopbackAndProtectedPorts(t *testing.T) {
	cfg, err := Parse(nil, "server")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Authorize("127.0.0.1", 3000) == nil {
		t.Fatal("default allowlist is open")
	}
	values, _ := structpb.NewStruct(map[string]any{"allow_ports": []any{3000.0, 9527.0}})
	cfg, err = Parse(values, "server")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Authorize("127.0.0.1", 3000) != nil || cfg.Authorize("::1", 3000) != nil {
		t.Fatal("loopback rejected")
	}
	for _, address := range []string{"localhost", "0.0.0.0", "::", "192.168.1.1", "8.8.8.8", "127.0.0.1%en0"} {
		if cfg.Authorize(address, 3000) == nil {
			t.Fatal("nonliteral/nonloopback accepted", address)
		}
	}
	if cfg.Authorize("127.0.0.1", 9527) == nil {
		t.Fatal("protected port accepted")
	}
	values.Fields["allow_protected"] = structpb.NewBoolValue(true)
	cfg, err = Parse(values, "server")
	if err != nil || cfg.Authorize("127.0.0.1", 9527) != nil {
		t.Fatal(err)
	}
}
func TestConfigurationRejectsUnknownAndWrongSide(t *testing.T) {
	for _, tc := range []struct {
		side  string
		value map[string]any
	}{{"server", map[string]any{"unknown": true}}, {"client", map[string]any{"allow_ports": []any{3000.0}}}, {"server", map[string]any{"fallback_port_on_conflict": true}}, {"server", map[string]any{"allow_ports": []any{1.5}}}, {"server", map[string]any{"allow_ports": []any{65536.0}}}, {"server", map[string]any{"max_connections": 100000.0}}, {"client", map[string]any{"idle_timeout_seconds": 0.0}}, {"client", map[string]any{"allow_protected": true}}} {
		v, _ := structpb.NewStruct(tc.value)
		if _, err := Parse(v, tc.side); err == nil {
			t.Fatal("accepted bad config", tc)
		}
	}
}

func TestDocumentedDefaultsAndExplicitAutomation(t *testing.T) {
	v, _ := structpb.NewStruct(map[string]any{"auto_forward": false, "bind_address": "127.0.0.1"})
	c, err := Parse(v, "client")
	if err != nil || c.BindAddress != "127.0.0.1" {
		t.Fatal(c, err)
	}
	v.Fields["auto_forward"] = structpb.NewBoolValue(true)
	if c, err = Parse(v, "client"); err != nil || !c.AutoForward || len(c.AutoAllowed) != 0 {
		t.Fatal("automation must retain default-deny policy", c, err)
	}
	v.Fields["auto_forward"] = structpb.NewBoolValue(false)
	v.Fields["bind_address"] = structpb.NewStringValue("0.0.0.0")
	if _, err = Parse(v, "client"); err == nil {
		t.Fatal("wildcard bind accepted")
	}
}

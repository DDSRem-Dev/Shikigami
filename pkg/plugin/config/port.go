// Package config defines built-in plugin configuration contracts shared by hosts and workers.
package config

import (
	"fmt"
	"google.golang.org/protobuf/types/known/structpb"
	"math"
	"net/netip"
	"time"
)

type Config struct {
	Allowed                                map[uint16]bool
	AutoAllowed                            map[uint16]bool
	AutoForward                            bool
	AllowProtected                         bool
	Fallback                               bool
	BindAddress                            string
	MaxConnections                         int
	DialTimeout, IdleTimeout, ScanInterval time.Duration
}

func Parse(v *structpb.Struct, side string) (Config, error) {
	if side != "client" && side != "server" {
		return Config{}, fmt.Errorf("invalid port plugin side")
	}
	c := Config{BindAddress: "127.0.0.1", Allowed: map[uint16]bool{}, AutoAllowed: map[uint16]bool{}, MaxConnections: 128, DialTimeout: 5 * time.Second, IdleTimeout: 5 * time.Minute, ScanInterval: 2 * time.Second}
	for k, v := range v.GetFields() {
		switch k {
		case "auto_forward":
			b, ok := v.Kind.(*structpb.Value_BoolValue)
			if !ok {
				return c, fmt.Errorf("auto_forward must be boolean")
			}
			if side != "client" {
				return c, fmt.Errorf("auto_forward is client-only")
			}
			c.AutoForward = b.BoolValue
		case "bind_address":
			if side != "client" {
				return c, fmt.Errorf("bind_address is client-only")
			}
			text, ok := v.Kind.(*structpb.Value_StringValue)
			if !ok {
				return c, fmt.Errorf("bind_address must be a literal loopback address")
			}
			address, err := Loopback(text.StringValue)
			if err != nil {
				return c, err
			}
			c.BindAddress = address.String()
		case "allow_ports", "auto_allow_ports":
			if (k == "allow_ports" && side != "server") || (k == "auto_allow_ports" && side != "client") {
				return c, fmt.Errorf("allow_ports is server-only")
			}
			list, ok := v.Kind.(*structpb.Value_ListValue)
			if !ok {
				return c, fmt.Errorf("allow_ports must be a list")
			}
			for _, n := range list.ListValue.Values {
				number, ok := n.Kind.(*structpb.Value_NumberValue)
				if !ok || number.NumberValue != math.Trunc(number.NumberValue) || number.NumberValue < 1 || number.NumberValue > 65535 {
					return c, fmt.Errorf("invalid allowed port")
				}
				if k == "allow_ports" {
					c.Allowed[uint16(number.NumberValue)] = true
				} else {
					c.AutoAllowed[uint16(number.NumberValue)] = true
				}
			}
		case "allow_protected", "fallback_port_on_conflict":
			b, ok := v.Kind.(*structpb.Value_BoolValue)
			if !ok {
				return c, fmt.Errorf("%s must be boolean", k)
			}
			if k == "allow_protected" && side == "server" {
				c.AllowProtected = b.BoolValue
			} else if k == "fallback_port_on_conflict" && side == "client" {
				c.Fallback = b.BoolValue
			} else {
				return c, fmt.Errorf("%s is invalid on %s", k, side)
			}
		case "max_connections", "dial_timeout_seconds", "idle_timeout_seconds", "scan_interval_seconds":
			n, ok := v.Kind.(*structpb.Value_NumberValue)
			if !ok || n.NumberValue != math.Trunc(n.NumberValue) || n.NumberValue < 1 || n.NumberValue > 86400 {
				return c, fmt.Errorf("invalid %s", k)
			}
			number := int(n.NumberValue)
			switch k {
			case "max_connections":
				if number > 4096 {
					return c, fmt.Errorf("too many connections")
				}
				c.MaxConnections = number
			case "dial_timeout_seconds":
				c.DialTimeout = time.Duration(number) * time.Second
			case "idle_timeout_seconds":
				c.IdleTimeout = time.Duration(number) * time.Second
			case "scan_interval_seconds":
				if side != "server" {
					return c, fmt.Errorf("scan interval is server-only")
				}
				c.ScanInterval = time.Duration(number) * time.Second
			}
		default:
			return c, fmt.Errorf("unsupported port configuration: %s", k)
		}
	}
	return c, nil
}

var protected = map[uint16]bool{22: true, 53: true, 2375: true, 2376: true, 3306: true, 5432: true, 6379: true, 6443: true, 9200: true, 9527: true, 11211: true, 27017: true}

func (c Config) Authorize(address string, port int32) error {
	if _, err := Loopback(address); err != nil {
		return err
	}
	if port < 1 || port > 65535 || !c.Allowed[uint16(port)] {
		return fmt.Errorf("port not allowlisted")
	}
	if protected[uint16(port)] && !c.AllowProtected {
		return fmt.Errorf("protected port requires explicit override")
	}
	return nil
}

// Literal addresses avoid DNS rebinding and disable arbitrary network proxying.
func Loopback(address string) (netip.Addr, error) {
	a, err := netip.ParseAddr(address)
	if err != nil || a.Zone() != "" || !a.IsLoopback() {
		return netip.Addr{}, fmt.Errorf("literal loopback address required")
	}
	return a.Unmap(), nil
}

func Protected(port uint16) bool { return protected[port] }

func (c Config) AutomaticAllowed(port uint32) bool {
	return c.AutoForward && port > 0 && port <= 65535 && c.AutoAllowed[uint16(port)] && !Protected(uint16(port))
}

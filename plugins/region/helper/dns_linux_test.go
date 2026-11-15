//go:build linux

package helper

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"sync"
	"testing"
	"time"
)

// net.Pipe implements a deterministic in-memory DNS exchange. The PacketConn
// methods select the resolver's UDP packet framing without opening any socket.
type dnsPacketFixture struct {
	net.Conn
	timeout bool
}

func (c *dnsPacketFixture) Read(data []byte) (int, error) {
	if c.timeout {
		return 0, os.ErrDeadlineExceeded
	}
	return c.Conn.Read(data)
}
func (c *dnsPacketFixture) ReadFrom(data []byte) (int, net.Addr, error) {
	count, err := c.Read(data)
	return count, c.RemoteAddr(), err
}
func (c *dnsPacketFixture) WriteTo(data []byte, _ net.Addr) (int, error) {
	return c.Write(data)
}

type dnsExchangeFixture struct {
	mu       sync.Mutex
	queries  map[string]int
	modes    map[string]string
	failures []error
	pending  sync.WaitGroup
}

func (f *dnsExchangeFixture) dial(_ context.Context, network, address string) (net.Conn, error) {
	mode, allowed := f.modes[address]
	if !allowed || (network != "udp" && network != "tcp") {
		return nil, fmt.Errorf("DNS test rejected unauthorized destination %s/%s", network, address)
	}
	client, server := net.Pipe()
	f.pending.Add(1)
	go func() {
		defer f.pending.Done()
		defer server.Close()
		_ = server.SetDeadline(time.Now().Add(3 * time.Second))
		query := make([]byte, 4096)
		count, err := server.Read(query)
		if err != nil {
			return // resolver cancellation may close another concurrent A/AAAA attempt
		}
		query = query[:count]
		f.mu.Lock()
		f.queries[address]++
		f.mu.Unlock()
		if mode == "timeout" || mode == "eof" {
			return // dialing succeeded and the DNS request was actually written
		}
		response, err := fixtureDNSResponse(query, mode)
		if err != nil {
			f.mu.Lock()
			f.failures = append(f.failures, err)
			f.mu.Unlock()
			return
		}
		_, _ = server.Write(response)
	}()
	return &dnsPacketFixture{Conn: client, timeout: mode == "timeout"}, nil
}

func fixtureDNSResponse(query []byte, mode string) ([]byte, error) {
	if len(query) < 17 || binary.BigEndian.Uint16(query[4:6]) != 1 {
		return nil, errors.New("invalid fixture DNS question")
	}
	end := 12
	for {
		if end >= len(query) || query[end] > 63 {
			return nil, errors.New("invalid fixture DNS name")
		}
		length := int(query[end])
		end++
		if length == 0 {
			break
		}
		end += length
	}
	if end+4 > len(query) {
		return nil, errors.New("truncated fixture DNS question")
	}
	kind := binary.BigEndian.Uint16(query[end : end+2])
	response := append([]byte(nil), query[:end+4]...)
	response[2], response[3] = 0x81, 0x80
	for index := 6; index < 12; index++ {
		response[index] = 0
	}
	if mode == "servfail" {
		response[3] = 0x82
	} else if mode == "success" && kind == 1 {
		binary.BigEndian.PutUint16(response[6:8], 1)
		response = append(response, 0xc0, 0x0c, 0, 1, 0, 1, 0, 0, 0, 60, 0, 4, 93, 184, 216, 34)
	}
	return response, nil
}

func (f *dnsExchangeFixture) finish(t *testing.T) map[string]int {
	t.Helper()
	f.pending.Wait()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.failures) != 0 {
		t.Fatalf("DNS fixture could not exchange a real query: %v", f.failures)
	}
	return f.queries
}

func TestDNSFailoverRetriesAfterActualExchangeFailure(t *testing.T) {
	for _, failure := range []string{"timeout", "eof", "servfail", "empty"} {
		t.Run(failure, func(t *testing.T) {
			fixture := &dnsExchangeFixture{queries: make(map[string]int), modes: map[string]string{"192.0.2.1:53": failure, "192.0.2.2:53": "success"}}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			addresses, err := lookupDNSServers(ctx, []string{"192.0.2.1", "192.0.2.2"}, "authorized.example", fixture.dial)
			queries := fixture.finish(t)
			if err != nil || len(addresses) != 1 || addresses[0] != netip.MustParseAddr("93.184.216.34") {
				t.Fatalf("working secondary was not used after %s: %v, %v", failure, addresses, err)
			}
			if queries["192.0.2.1:53"] == 0 || queries["192.0.2.2:53"] == 0 || len(queries) != 2 {
				t.Fatalf("failover did not follow actual exchanges with only the authorized servers: %v", queries)
			}
		})
	}
}

func TestDNSFailoverStopsAfterSuccessOrExhaustion(t *testing.T) {
	for _, primaryWorks := range []bool{false, true} {
		t.Run(fmt.Sprint(primaryWorks), func(t *testing.T) {
			primary := "eof"
			if primaryWorks {
				primary = "success"
			}
			fixture := &dnsExchangeFixture{queries: make(map[string]int), modes: map[string]string{"192.0.2.1:53": primary, "192.0.2.2:53": "eof"}}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			addresses, err := lookupDNSServers(ctx, []string{"192.0.2.1", "192.0.2.2"}, "authorized.example", fixture.dial)
			queries := fixture.finish(t)
			if primaryWorks {
				if err != nil || len(addresses) != 1 || queries["192.0.2.2:53"] != 0 {
					t.Fatalf("successful primary triggered an unnecessary resolver fallback: %v, %v, %v", addresses, err, queries)
				}
			} else if err == nil || len(addresses) != 0 || queries["192.0.2.1:53"] == 0 || queries["192.0.2.2:53"] == 0 {
				t.Fatalf("failed resolver list silently succeeded or skipped a server: %v, %v, %v", addresses, err, queries)
			}
		})
	}
}

func TestDNSFailoverHonorsCanceledLookupWithoutDialing(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	calls := 0
	_, err := lookupDNSServers(ctx, []string{"192.0.2.1", "192.0.2.2"}, "authorized.example", func(context.Context, string, string) (net.Conn, error) {
		calls++
		return nil, errors.New("unexpected DNS dial")
	})
	if !errors.Is(err, context.Canceled) || calls != 0 {
		t.Fatalf("canceled lookup continued to another resolver: calls=%d, err=%v", calls, err)
	}
}

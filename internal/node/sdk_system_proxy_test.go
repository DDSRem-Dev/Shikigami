package node

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/net/http/httpproxy"
)

func proxyFixture(proxy string, extras string) []byte {
	u, _ := url.Parse(proxy)
	return []byte(fmt.Sprintf("<dictionary> {\n HTTPEnable : 1\n HTTPProxy : %s\n HTTPPort : %s\n HTTPSEnable : 1\n HTTPSProxy : %s\n HTTPSPort : %s\n%s\n}\n", u.Hostname(), u.Port(), u.Hostname(), u.Port(), extras))
}

func TestSystemProxySelectionAndExactBypass(t *testing.T) {
	raw := proxyFixture("http://127.0.0.1:7897", ` ExceptionsList : <array> {
  0 : exact.example
  1 : *.internal.example
  2 : 169.254/16
  3 : <local>
 }
 __SCOPED__ : <dictionary> {
  en0 : <dictionary> {
   HTTPEnable : 1
   HTTPProxy : wrong.example
   HTTPPort : 9999
  }
 }`)
	resolver, err := systemProxyResolver(&httpproxy.Config{HTTPProxy: "environment.example:8080", NoProxy: ".explicit.example"}, func() ([]byte, error) { return raw, nil })
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct{ target, want string }{
		{"http://public.example", "http://environment.example:8080"},
		{"https://public.example", "http://127.0.0.1:7897"},
		{"https://exact.example", ""},
		{"https://child.exact.example", "http://127.0.0.1:7897"},
		{"https://host.internal.example", ""},
		{"https://internal.example", "http://127.0.0.1:7897"},
		{"https://169.254.2.3", ""},
		{"https://printer", ""},
		{"https://host.explicit.example", ""},
		{"http://host.explicit.example", ""},
		{"https://localhost", ""},
		{"https://127.0.0.1", ""},
		{"https://[::1]", ""},
	} {
		t.Run(test.target, func(t *testing.T) {
			target, _ := url.Parse(test.target)
			actual, err := resolver(target)
			if err != nil {
				t.Fatal(err)
			}
			got := ""
			if actual != nil {
				got = actual.String()
			}
			if got != test.want {
				t.Fatalf("got %q want %q", got, test.want)
			}
		})
	}
	reads := 0
	resolver, err = systemProxyResolver(&httpproxy.Config{HTTPProxy: "http://env.example:8080", HTTPSProxy: "https://env.example:8443"}, func() ([]byte, error) { reads++; return nil, errors.New("must not read") })
	if err != nil || reads != 0 || resolver == nil {
		t.Fatal("explicit environment did not bypass system discovery")
	}
}

func TestSystemProxyRejectsUnsupportedAndMalformedConfiguration(t *testing.T) {
	for _, raw := range []string{
		"garbage",
		"<dictionary> {\n HTTPEnable : 1\n}",
		"<dictionary> {\n HTTPEnable : 2\n}",
		"<dictionary> {\n HTTPEnable : 0\n HTTPEnable : 1\n}",
		"<dictionary> {\n SOCKSEnable : 1\n}",
		"<dictionary> {\n ProxyAutoConfigEnable : 1\n}",
		"<dictionary> {\n ProxyAutoDiscoveryEnable : 1\n}",
		"<dictionary> {\n ExceptionsList : <array> {\n 0 : host?.example\n }\n}",
	} {
		if _, err := parseSystemHTTPProxy([]byte(raw)); err == nil {
			t.Fatal("accepted invalid or unsupported system configuration")
		}
	}
	for _, proxy := range []string{"socks5://127.0.0.1:1080", "http://user:secret@localhost:8080", "http://host:0", "http://host:65536", "http://host/path", "http://host?secret", "http://host#fragment", "http://host:"} {
		_, err := systemProxyResolver(&httpproxy.Config{HTTPProxy: proxy, HTTPSProxy: proxy}, func() ([]byte, error) { t.Fatal("invalid environment must fail before discovery"); return nil, nil })
		if err == nil || strings.Contains(err.Error(), "secret") {
			t.Fatal("environment proxy was not safely rejected")
		}
	}
	disabled, err := parseSystemHTTPProxy([]byte("<dictionary> {\n HTTPEnable : 0\n HTTPProxy : unused\n HTTPPort : 0\n}"))
	if err != nil || disabled.http != nil {
		t.Fatal("disabled system proxy was fabricated")
	}
}

func TestEnvironmentProxyPrecedenceSurvivesUnusedSystemFailure(t *testing.T) {
	for _, read := range []func() ([]byte, error){
		func() ([]byte, error) { return nil, errors.New("private command details") },
		func() ([]byte, error) { return []byte("<dictionary> {\n ProxyAutoConfigEnable : 1\n}"), nil },
	} {
		resolver, err := systemProxyResolver(&httpproxy.Config{HTTPSProxy: "https://explicit.example:8443", NoProxy: "bypass.example"}, read)
		if err != nil {
			t.Fatal("unused system failure blocked explicit HTTPS proxy")
		}
		secure, _ := url.Parse("https://public.example")
		proxy, err := resolver(secure)
		if err != nil || proxy == nil || proxy.Host != "explicit.example:8443" {
			t.Fatal("explicit scheme precedence lost")
		}
		bypassed, _ := url.Parse("http://bypass.example")
		proxy, err = resolver(bypassed)
		if err != nil || proxy != nil {
			t.Fatal("NO_PROXY fell through into broken system discovery")
		}
		fallback, _ := url.Parse("http://public.example")
		if _, err := resolver(fallback); err == nil || strings.Contains(err.Error(), "private") {
			t.Fatal("unsupported fallback was not safely reported")
		}
	}
	resolver, err := systemProxyResolver(&httpproxy.Config{NoProxy: "localhost, *"}, func() ([]byte, error) { t.Fatal("NO_PROXY=* must skip discovery"); return nil, nil })
	if err != nil {
		t.Fatal(err)
	}
	target, _ := url.Parse("https://public.example")
	if proxy, err := resolver(target); err != nil || proxy != nil {
		t.Fatal("NO_PROXY=* did not bypass")
	}
	resolver, err = systemProxyResolver(&httpproxy.Config{HTTPProxy: "http://explicit.example:8080"}, func() ([]byte, error) {
		return []byte("<dictionary> {\n HTTPEnable : malformed-but-overridden\n HTTPPort : invalid\n HTTPSEnable : 1\n HTTPSProxy : system.example\n HTTPSPort : 8443\n}"), nil
	})
	if err != nil {
		t.Fatal("overridden HTTP fields blocked valid system HTTPS")
	}
	if proxy, err := resolver(target); err != nil || proxy == nil || proxy.Host != "system.example:8443" {
		t.Fatal("valid uncovered system scheme was not retained")
	}
}

func TestSystemHTTPProxyCONNECTPreservesTLSAndLoopbackBypass(t *testing.T) {
	origin := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, "encrypted origin reply") }))
	origin.Config.ErrorLog = log.New(io.Discard, "", 0)
	origin.StartTLS()
	defer origin.Close()
	var connects atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodConnect {
			http.Error(w, "CONNECT required", 400)
			return
		}
		upstream, err := net.DialTimeout("tcp", origin.Listener.Addr().String(), time.Second)
		if err != nil {
			http.Error(w, "unavailable", 502)
			return
		}
		client, buffer, err := w.(http.Hijacker).Hijack()
		if err != nil {
			upstream.Close()
			return
		}
		connects.Add(1)
		_, _ = buffer.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n")
		_ = buffer.Flush()
		done := make(chan struct{}, 1)
		go func() { _, _ = io.Copy(upstream, buffer); done <- struct{}{} }()
		_, _ = io.Copy(client, upstream)
		_ = client.Close()
		_ = upstream.Close()
		<-done
	}))
	defer proxy.Close()
	resolver, err := systemProxyResolver(&httpproxy.Config{}, func() ([]byte, error) { return proxyFixture(proxy.URL, ""), nil })
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(origin.Certificate())
	transport := &http.Transport{Proxy: func(r *http.Request) (*url.URL, error) { return resolver(r.URL) }, TLSClientConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}, DisableKeepAlives: true}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 3 * time.Second}
	_, port, _ := net.SplitHostPort(origin.Listener.Addr().String())
	name := origin.Certificate().DNSNames[0]
	response, err := client.Get("https://" + net.JoinHostPort(name, port) + "/via-proxy")
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil || string(body) != "encrypted origin reply" || connects.Load() != 1 {
		t.Fatal("CONNECT request did not reach TLS origin")
	}
	_, err = client.Get("https://" + net.JoinHostPort("wrong-name.invalid", port) + "/must-fail")
	var nameError x509.HostnameError
	if !errors.As(err, &nameError) {
		t.Fatal("proxy bypassed target TLS hostname validation")
	}
	before := connects.Load()
	response, err = client.Get(origin.URL + "/direct-loopback")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if connects.Load() != before {
		t.Fatal("loopback incorrectly traversed system proxy")
	}
}

func TestProxyCommandHelper(t *testing.T) {
	mode := os.Getenv("SHIKI_PROXY_COMMAND_TEST")
	if mode == "" {
		return
	}
	switch mode {
	case "overflow":
		_, _ = os.Stdout.Write([]byte(strings.Repeat("x", maxSystemProxyOutput+1)))
	case "stderr":
		_, _ = os.Stderr.Write([]byte(strings.Repeat("x", maxSystemProxyOutput+1)))
	case "wait":
		time.Sleep(10 * time.Second)
	case "ok":
		_, _ = io.WriteString(os.Stdout, "<dictionary> {\n}\n")
	}
	os.Exit(0)
}

func TestProxyCommandBoundsAndCancellation(t *testing.T) {
	for _, mode := range []string{"overflow", "stderr", "wait", "ok"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("SHIKI_PROXY_COMMAND_TEST", mode)
			t.Setenv("GORACE", "atexit_sleep_ms=0")
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			start := time.Now()
			data, err := readProxyCommand(ctx, os.Args[0], "-test.run=^TestProxyCommandHelper$")
			if mode == "ok" {
				if err != nil || string(data) != "<dictionary> {\n}\n" {
					t.Fatal("bounded helper output failed")
				}
			} else if err != errSystemProxyRead || len(data) != 0 {
				t.Fatal("command failure exposed output or lost error")
			}
			if time.Since(start) > 3*time.Second {
				t.Fatal("command was not canceled promptly")
			}
		})
	}
}

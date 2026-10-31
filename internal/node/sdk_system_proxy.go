package node

import (
	"errors"
	"net"
	"net/netip"
	"net/url"
	"path"
	"strconv"
	"strings"

	"golang.org/x/net/http/httpproxy"
)

var (
	errSystemProxyInvalid     = errors.New("node system HTTP proxy configuration is invalid")
	errSystemProxyUnsupported = errors.New("node system proxy mode is unsupported")
	errSystemProxyRead        = errors.New("node system HTTP proxy configuration unavailable")
	errSDKProxyInitialization = errors.New("node SDK proxy policy could not be initialized")
)

type systemHTTPProxy struct {
	http, https *url.URL
	bypass      []string
	simpleHosts bool
}

// parseSystemHTTPProxy accepts only the global static HTTP(S) configuration.
// Scoped/supplemental dictionaries cannot overwrite the global values. No
// authentication material, PAC scripts, or SOCKS settings are interpreted.
func parseSystemHTTPProxy(raw []byte) (systemHTTPProxy, error) {
	return parseSystemHTTPProxyForSchemes(raw, true, true)
}

func parseSystemHTTPProxyForSchemes(raw []byte, wantHTTP, wantHTTPS bool) (systemHTTPProxy, error) {
	var result systemHTTPProxy
	values := map[string]string{}
	depth, exceptions, opened, closed := 0, false, false, false
	known := map[string]bool{"HTTPEnable": true, "HTTPProxy": true, "HTTPPort": true, "HTTPSEnable": true, "HTTPSProxy": true, "HTTPSPort": true, "SOCKSEnable": true, "ProxyAutoConfigEnable": true, "ProxyAutoDiscoveryEnable": true, "ExcludeSimpleHostnames": true}
	for _, original := range strings.Split(string(raw), "\n") {
		line := strings.TrimSpace(original)
		if line == "" {
			continue
		}
		if !opened {
			if line != "<dictionary> {" {
				return result, errSystemProxyInvalid
			}
			opened, depth = true, 1
			continue
		}
		if closed {
			return result, errSystemProxyInvalid
		}
		if line == "}" {
			if depth == 2 {
				exceptions = false
			}
			depth--
			if depth == 0 {
				closed = true
			}
			continue
		}
		key, value, ok := strings.Cut(line, " : ")
		if !ok {
			return result, errSystemProxyInvalid
		}
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		container := strings.HasSuffix(value, " {")
		if depth == 1 && (known[key] || key == "ExceptionsList") {
			if _, duplicate := values[key]; duplicate {
				return result, errSystemProxyInvalid
			}
			values[key] = value
			if key == "ExceptionsList" {
				if value != "<array> {" {
					return result, errSystemProxyInvalid
				}
				exceptions = true
			} else if container {
				return result, errSystemProxyInvalid
			}
		} else if depth == 2 && exceptions {
			if _, err := strconv.ParseUint(key, 10, 32); err != nil || container || !validSystemBypass(value) {
				return result, errSystemProxyInvalid
			}
			result.bypass = append(result.bypass, value)
		}
		if container {
			depth++
		}
	}
	if !opened || !closed || depth != 0 {
		return result, errSystemProxyInvalid
	}
	flag := func(name string) (bool, error) {
		value := values[name]
		if value == "" || value == "0" {
			return false, nil
		}
		if value == "1" {
			return true, nil
		}
		return false, errSystemProxyInvalid
	}
	for _, key := range []string{"ProxyAutoConfigEnable", "ProxyAutoDiscoveryEnable"} {
		on, err := flag(key)
		if err != nil {
			return result, err
		}
		if on {
			return result, errSystemProxyUnsupported
		}
	}
	var err error
	result.simpleHosts, err = flag("ExcludeSimpleHostnames")
	if err != nil {
		return result, err
	}
	for _, scheme := range []string{"HTTP", "HTTPS"} {
		if (scheme == "HTTP" && !wantHTTP) || (scheme == "HTTPS" && !wantHTTPS) {
			continue
		}
		on, err := flag(scheme + "Enable")
		if err != nil {
			return result, err
		}
		if !on {
			continue
		}
		host := values[scheme+"Proxy"]
		if host == "" || strings.ContainsAny(host, " /\\@?#\t\r\n") {
			return result, errSystemProxyInvalid
		}
		if strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") {
			host = host[1 : len(host)-1]
		}
		if strings.Contains(host, ":") {
			if ip, e := netip.ParseAddr(host); e != nil || !ip.Is6() {
				return result, errSystemProxyInvalid
			}
		}
		port, err := strconv.ParseUint(values[scheme+"Port"], 10, 16)
		if err != nil || port == 0 {
			return result, errSystemProxyInvalid
		}
		proxy := &url.URL{Scheme: "http", Host: net.JoinHostPort(host, strconv.FormatUint(port, 10))}
		if scheme == "HTTP" {
			result.http = proxy
		} else {
			result.https = proxy
		}
	}
	socks, err := flag("SOCKSEnable")
	if err != nil {
		return result, err
	}
	if socks && result.http == nil && result.https == nil {
		return result, errSystemProxyUnsupported
	}
	return result, nil
}

func systemBypassPrefix(pattern string) (netip.Prefix, bool) {
	if prefix, err := netip.ParsePrefix(pattern); err == nil {
		return prefix, true
	}
	// System Settings commonly writes short IPv4 networks such as 169.254/16.
	host, bits, ok := strings.Cut(pattern, "/")
	if !ok || strings.Contains(host, ":") {
		return netip.Prefix{}, false
	}
	parts := strings.Split(host, ".")
	if len(parts) > 4 {
		return netip.Prefix{}, false
	}
	for _, part := range parts {
		if _, err := strconv.ParseUint(part, 10, 8); err != nil {
			return netip.Prefix{}, false
		}
	}
	for len(parts) < 4 {
		parts = append(parts, "0")
	}
	prefix, err := netip.ParsePrefix(strings.Join(parts, ".") + "/" + bits)
	return prefix, err == nil
}

func validSystemBypass(pattern string) bool {
	if pattern == "<local>" {
		return true
	}
	if _, ok := systemBypassPrefix(pattern); ok {
		return true
	}
	if pattern == "" || strings.ContainsAny(pattern, " /\\@?#[],\t\r\n") {
		return false
	}
	if strings.Contains(pattern, ":") {
		_, err := netip.ParseAddr(pattern)
		return err == nil
	}
	return true
}

func (p systemHTTPProxy) bypasses(host string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	ip, ipErr := netip.ParseAddr(host)
	simple := ipErr != nil && !strings.Contains(host, ".")
	if p.simpleHosts && simple {
		return true
	}
	for _, pattern := range p.bypass {
		if pattern == "<local>" && simple {
			return true
		}
		if prefix, ok := systemBypassPrefix(pattern); ok {
			if ipErr == nil && prefix.Contains(ip) {
				return true
			}
			continue
		}
		if matched, _ := path.Match(strings.ToLower(pattern), host); matched {
			return true
		}
	}
	return false
}

// systemProxyResolver captures one process-start snapshot. Environment proxy
// selection and NO_PROXY retain Go's existing semantics. Apple exceptions use
// explicit host/glob/CIDR matching, not Go's broader domain-suffix convention.
func systemProxyResolver(environment *httpproxy.Config, read func() ([]byte, error)) (func(*url.URL) (*url.URL, error), error) {
	copyEnv := *environment
	for _, value := range []*string{&copyEnv.HTTPProxy, &copyEnv.HTTPSProxy} {
		if *value == "" {
			continue
		}
		proxy, err := supportedEnvironmentProxy(*value)
		if err != nil {
			return nil, err
		}
		*value = proxy.String()
	}
	envProxy := copyEnv.ProxyFunc()
	bypassConfig := httpproxy.Config{HTTPProxy: "http://bypass.invalid:80", HTTPSProxy: "http://bypass.invalid:80", NoProxy: copyEnv.NoProxy}
	envBypass := bypassConfig.ProxyFunc()
	var system systemHTTPProxy
	var fallbackError error
	bypassAll := false
	for _, entry := range strings.Split(copyEnv.NoProxy, ",") {
		if strings.TrimSpace(entry) == "*" {
			bypassAll = true
		}
	}
	if !bypassAll && (copyEnv.HTTPProxy == "" || copyEnv.HTTPSProxy == "") {
		raw, err := read()
		if err != nil {
			fallbackError = errSystemProxyRead
		} else {
			system, fallbackError = parseSystemHTTPProxyForSchemes(raw, copyEnv.HTTPProxy == "", copyEnv.HTTPSProxy == "")
		}
		if fallbackError != nil && copyEnv.HTTPProxy == "" && copyEnv.HTTPSProxy == "" {
			return nil, fallbackError
		}
	}
	return func(target *url.URL) (*url.URL, error) {
		if target == nil || (target.Scheme != "http" && target.Scheme != "https") {
			return nil, nil
		}
		bypass, err := envBypass(target)
		if err != nil {
			return nil, errSystemProxyInvalid
		}
		if bypass == nil {
			return nil, nil
		}
		if (target.Scheme == "http" && copyEnv.HTTPProxy != "") || (target.Scheme == "https" && copyEnv.HTTPSProxy != "") {
			proxy, err := envProxy(target)
			if err != nil {
				return nil, errSystemProxyInvalid
			}
			return proxy, nil
		}
		if fallbackError != nil {
			return nil, fallbackError
		}
		if system.bypasses(target.Hostname()) {
			return nil, nil
		}
		proxy := system.http
		if target.Scheme == "https" {
			proxy = system.https
		}
		if proxy == nil {
			return nil, nil
		}
		copyURL := *proxy
		return &copyURL, nil
	}, nil
}

func supportedEnvironmentProxy(value string) (*url.URL, error) {
	if strings.ContainsAny(value, " \t\r\n\x00") {
		return nil, errSystemProxyInvalid
	}
	if !strings.Contains(value, "://") {
		value = "http://" + value
	}
	proxy, err := url.Parse(value)
	if err != nil || proxy.Hostname() == "" {
		return nil, errSystemProxyInvalid
	}
	proxy.Scheme = strings.ToLower(proxy.Scheme)
	if proxy.User != nil || (proxy.Scheme != "http" && proxy.Scheme != "https") {
		return nil, errSystemProxyUnsupported
	}
	if proxy.Path != "" || proxy.RawQuery != "" || proxy.Fragment != "" || proxy.Opaque != "" {
		return nil, errSystemProxyInvalid
	}
	if port := proxy.Port(); port != "" {
		if number, err := strconv.ParseUint(port, 10, 16); err != nil || number == 0 {
			return nil, errSystemProxyInvalid
		}
	} else if strings.HasSuffix(proxy.Host, ":") {
		return nil, errSystemProxyInvalid
	}
	if strings.Contains(proxy.Hostname(), ":") {
		if _, err := netip.ParseAddr(proxy.Hostname()); err != nil {
			return nil, errSystemProxyInvalid
		}
	}
	return proxy, nil
}

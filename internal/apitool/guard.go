package apitool

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"sync"
)

// HostNotAllowedError: the secret variable Var would go to Host, which is not
// in its allowed hosts (C3.10 layer 3). Nothing was sent.
type HostNotAllowedError struct{ Var, Host string }

func (e *HostNotAllowedError) Error() string {
	return fmt.Sprintf("apitool: {{%s}} is not allowed for host %s", e.Var, e.Host)
}

// PrivateTargetError: a request someone else changed last goes to this PC or
// its network (Host, resolved IP when known). Nothing was sent; with consent
// the caller sets Prepared.PrivateOK and sends again.
type PrivateTargetError struct {
	Host   string
	IP     netip.Addr
	LastBy string
}

func (e *PrivateTargetError) Error() string {
	return "apitool: private target " + e.Host
}

// RedirectBlock records a redirect not followed because the next host is not
// allowed for the secret Var; the 3xx response is returned as is.
type RedirectBlock struct {
	To       string // host name
	Var      string
	Insecure bool // an https request using a secret redirected to plain http
}

// normHost lower-cases a host, drops a port, brackets and a final dot.
func normHost(h string) string {
	h = strings.TrimSpace(h)
	if host, _, err := net.SplitHostPort(h); err == nil {
		h = host
	}
	h = strings.TrimSuffix(strings.TrimPrefix(h, "["), "]")
	return strings.TrimSuffix(strings.ToLower(h), ".")
}

func hostAllowed(host string, allowed []string) bool {
	host = normHost(host)
	for _, a := range allowed {
		if normHost(a) == host && host != "" {
			return true
		}
	}
	return false
}

func urlHost(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return normHost(u.Hostname())
}

// CheckSecretHosts checks, before any connection, that every secret p uses
// may go to p's host (exact host name, case and port ignored). A Host header
// row names a virtual host on that server, so it must be allowed too.
func CheckSecretHosts(p *Prepared) error {
	return checkHosts(p.Used, urlHost(p.URL), p.Header.Get("Host"))
}

// checkHosts: every secret in used may go to each non-empty host.
func checkHosts(used []Value, hosts ...string) error {
	for _, h := range hosts {
		h = normHost(h)
		if h == "" {
			continue
		}
		for _, v := range used {
			if v.Secret && !hostAllowed(h, v.AllowedHosts) {
				return &HostNotAllowedError{Var: v.Name, Host: h}
			}
		}
	}
	return nil
}

// DefaultHosts is the automatic allowed-host list when a secret's value is
// first entered: the request's host and the base_url host, without repeats.
func DefaultHosts(p *Prepared, s *Scope, d Dyn) []string {
	var out []string
	add := func(h string) {
		if h != "" && !hostAllowed(h, out) {
			out = append(out, h)
		}
	}
	if p != nil {
		add(urlHost(p.URL))
	}
	if s != nil {
		if v, err := s.Lookup("base_url"); err == nil && !v.Secret {
			raw := v.Text
			if !strings.Contains(raw, "://") {
				raw = "http://" + raw
			}
			add(urlHost(raw))
		}
	}
	return out
}

var privatePrefixes = func() []netip.Prefix {
	var out []netip.Prefix
	for _, s := range []string{
		"0.0.0.0/8", "10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8", "169.254.0.0/16",
		"172.16.0.0/12", "192.168.0.0/16", "::1/128", "::/128", "fc00::/7", "fe80::/10", "fec0::/10",
	} {
		out = append(out, netip.MustParsePrefix(s))
	}
	return out
}()

var nat64 = netip.MustParsePrefix("64:ff9b::/96")

// IsPrivateAddr: loopback, RFC 1918, CGNAT 100.64/10, link-local (cloud
// metadata 169.254.169.254), 0/8, ULA fc00::/7, fe80::/10, unspecified;
// IPv4-mapped and NAT64 addresses are judged by their IPv4 part.
func IsPrivateAddr(a netip.Addr) bool {
	a = a.Unmap()
	if a.Is6() && nat64.Contains(a) {
		b := a.As16()
		a = netip.AddrFrom4([4]byte{b[12], b[13], b[14], b[15]})
	}
	if !a.IsValid() || a.IsUnspecified() || a.IsLoopback() || a.IsPrivate() || a.IsLinkLocalUnicast() ||
		a.IsLinkLocalMulticast() || a.IsInterfaceLocalMulticast() {
		return true
	}
	for _, p := range privatePrefixes {
		if p.Contains(a) {
			return true
		}
	}
	return false
}

// IsPrivateHostname: localhost, *.localhost, *.local, a private IP literal,
// or a non-canonical IPv4 form ("2130706433", "127.1", "0x7f.1", "0177.0.0.1"):
// a proxy or the OS resolver may read it as any address (inet_aton), so it
// is never taken for a public name.
func IsPrivateHostname(h string) bool {
	h = normHost(h)
	if h == "localhost" || strings.HasSuffix(h, ".localhost") || strings.HasSuffix(h, ".local") {
		return true
	}
	if a, err := netip.ParseAddr(h); err == nil {
		return IsPrivateAddr(a)
	}
	return numericLastLabel(h)
}

// numericLastLabel: the last dot label is a decimal or 0x-hex number, which
// makes the host an IPv4 address to URL parsers and inet_aton (a public DNS
// name never ends in a number).
func numericLastLabel(h string) bool {
	last := h[strings.LastIndexByte(h, '.')+1:]
	digits := "0123456789"
	if len(last) >= 2 && last[0] == '0' && (last[1] == 'x' || last[1] == 'X') {
		last, digits = last[2:], "0123456789abcdefABCDEF"
		if last == "" {
			return true
		}
	}
	if last == "" {
		return false
	}
	for i := 0; i < len(last); i++ {
		if !strings.ContainsRune(digits, rune(last[i])) {
			return false
		}
	}
	return true
}

// guard is the per-send state of the target checks.
type guard struct {
	s      *Sender
	p      *Prepared
	target string // the request's own host (PrivateOK covers this one only)

	mu      sync.Mutex
	proxies map[string]bool // proxy "host:port" in use (not the target)
	block   *RedirectBlock
}

func newGuard(s *Sender, p *Prepared) *guard {
	return &guard{s: s, p: p, target: urlHost(p.URL), proxies: map[string]bool{}}
}

// checks reports whether host must be checked for a private target.
func (g *guard) checks(host string) bool {
	if !g.p.Foreign {
		return false
	}
	return !(g.p.PrivateOK && normHost(host) == g.target)
}

func (g *guard) privateErr(host string, ip netip.Addr) error {
	return &PrivateTargetError{Host: normHost(host), IP: ip, LastBy: g.p.LastBy}
}

// preflight is the no-connection literal check of a host (also when the
// request goes through a proxy, which resolves the name itself).
func (g *guard) preflight(host string) error {
	if !g.checks(host) || !IsPrivateHostname(host) {
		return nil
	}
	var ip netip.Addr
	if a, err := netip.ParseAddr(normHost(host)); err == nil {
		ip = a.Unmap()
	}
	return g.privateErr(host, ip)
}

// proxyFromEnv picks the proxy (tests replace it: net/http reads the
// environment only once per process).
var proxyFromEnv = http.ProxyFromEnvironment

// proxy is http.ProxyFromEnvironment that remembers the proxy host and checks
// a proxied target: its literal host, then (best effort, the proxy resolves
// the name itself) a local lookup of a checked name — any private address
// asks. A failed local lookup lets the request go to the proxy.
func (g *guard) proxy(req *http.Request) (*url.URL, error) {
	u, err := proxyFromEnv(req)
	if err != nil || u == nil {
		return u, err
	}
	host := req.URL.Hostname()
	if err := g.preflight(host); err != nil {
		return nil, err
	}
	if _, perr := netip.ParseAddr(normHost(host)); perr != nil && g.checks(host) {
		if ips, rerr := g.resolve(req.Context(), host); rerr == nil {
			for _, ip := range ips {
				if IsPrivateAddr(ip) {
					return nil, g.privateErr(host, ip.Unmap())
				}
			}
		}
	}
	g.mu.Lock()
	g.proxies[proxyAddr(u)] = true
	g.mu.Unlock()
	return u, nil
}

// proxyAddr is the "host:port" net/http dials for the proxy u (the scheme's
// default port when u has none).
func proxyAddr(u *url.URL) string {
	port := u.Port()
	if port == "" {
		switch strings.ToLower(u.Scheme) {
		case "https":
			port = "443"
		case "socks5", "socks5h":
			port = "1080"
		default:
			port = "80"
		}
	}
	return net.JoinHostPort(normHost(u.Hostname()), port)
}

// resolve looks host up with Sender.Resolver, else the system resolver.
func (g *guard) resolve(ctx context.Context, host string) ([]netip.Addr, error) {
	if g.s.Resolver != nil {
		return g.s.Resolver(ctx, host)
	}
	return net.DefaultResolver.LookupNetIP(ctx, "ip", host)
}

// dial connects to addr. A checked host (Foreign, not consented) is refused
// when its name or any resolved address is private; otherwise the resolved
// address itself is dialed, so a second lookup cannot swap in another IP.
func (g *guard) dial(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	g.mu.Lock()
	viaProxy := g.proxies[net.JoinHostPort(normHost(host), port)]
	g.mu.Unlock()
	check := g.checks(host) && !viaProxy
	d := &net.Dialer{}
	if check {
		if err := g.preflight(host); err != nil {
			return nil, err
		}
	}
	if a, err := netip.ParseAddr(normHost(host)); err == nil {
		if check && IsPrivateAddr(a) {
			return nil, g.privateErr(host, a.Unmap())
		}
		return d.DialContext(ctx, network, addr)
	}
	if !check && g.s.Resolver == nil {
		return d.DialContext(ctx, network, addr)
	}
	ips, err := g.resolve(ctx, host)
	if err != nil {
		return nil, err
	}
	if len(ips) == 0 {
		return nil, errors.New("apitool: no address")
	}
	if check {
		for _, ip := range ips {
			if IsPrivateAddr(ip) {
				return nil, g.privateErr(host, ip.Unmap())
			}
		}
	}
	var last error
	for _, ip := range ips {
		c, err := d.DialContext(ctx, network, net.JoinHostPort(ip.Unmap().String(), port))
		if err == nil {
			return c, nil
		}
		last = err
	}
	return nil, last
}

// redirect stops a redirect to a host outside the allowed hosts shared by
// every secret the request uses (net/http would keep X-Api-Key and the query),
// also when a relative redirect keeps a Host override (req.Host), and an
// https -> http redirect while a secret is in use (same host too).
func (g *guard) redirect(req *http.Request) error {
	host := normHost(req.URL.Hostname())
	if v, ok := firstSecret(g.p.Used); ok && downgrade(req) {
		g.mu.Lock()
		g.block = &RedirectBlock{To: host, Var: v.Name, Insecure: true}
		g.mu.Unlock()
		return http.ErrUseLastResponse
	}
	var he *HostNotAllowedError
	if err := checkHosts(g.p.Used, host, req.Host); errors.As(err, &he) {
		g.mu.Lock()
		g.block = &RedirectBlock{To: he.Host, Var: he.Var}
		g.mu.Unlock()
		return http.ErrUseLastResponse
	}
	return g.preflight(host)
}

// downgrade: req follows a redirect from an https request to plain http
// (net/http would resend X-Api-Key and the query in the clear).
func downgrade(req *http.Request) bool {
	if req.Response == nil || req.Response.Request == nil || req.Response.Request.URL == nil {
		return false
	}
	return strings.EqualFold(req.Response.Request.URL.Scheme, "https") && !strings.EqualFold(req.URL.Scheme, "https")
}

func firstSecret(used []Value) (Value, bool) {
	for _, v := range used {
		if v.Secret {
			return v, true
		}
	}
	return Value{}, false
}

func (g *guard) redirectBlock() *RedirectBlock {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.block
}

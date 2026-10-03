// Package urlvalidate validates link destinations.
//
// Three separate concerns, in order of how badly they fail:
//   - scheme allowlist: javascript:/data:/file: would make the shortener a
//     vector for delivering script to a victim's browser
//   - own-hostname rejection: prevents redirect loops
//   - private/loopback/link-local rejection: keeps the service from being
//     used to point at internal infrastructure, and closes the SSRF vector
//     for any future server-side fetching of destinations
package urlvalidate

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
)

var (
	ErrBadScheme    = errors.New("only http and https URLs are accepted")
	ErrNoHost       = errors.New("URL has no host")
	ErrOwnDomain    = errors.New("URL points at this service, which would create a redirect loop")
	ErrPrivateAddr  = errors.New("URL resolves to a private, loopback, or link-local address")
	ErrUnresolvable = errors.New("URL host could not be resolved")
)

// Validator holds the hostnames this service answers on, so it can reject
// destinations that point back at itself.
type Validator struct {
	ownHosts []string
	// resolve is injectable so tests can exercise the private-address branch
	// without depending on real DNS.
	resolve func(host string) ([]net.IP, error)
}

func New(ownHosts []string) *Validator {
	return &Validator{ownHosts: ownHosts, resolve: net.LookupIP}
}

// Normalize adds a scheme to input that has none, so `example.com/page`
// becomes `https://example.com/page`. Applied before parsing:
// url.Parse treats a scheme-less string as a path, not a host.
//
// Testing for "://" is not enough: `javascript:alert(1)` has no "://", so
// prepending a scheme would produce `https://javascript:alert(1)`, which
// fails to parse as an invalid port. The destination would still be
// rejected, but as a malformed URL rather than as a forbidden scheme — the
// wrong error for the caller and the wrong message for the user.
//
// So detect any scheme, not just one followed by "//". The wrinkle is that
// `example.com:8080/path` also parses as scheme `example.com`, because URL
// schemes may contain dots. No scheme in practical use does, while every
// bare host:port does, so a dot in the parsed scheme means it is really a
// hostname and the input still needs one prepended.
func Normalize(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return raw
	}

	// Protocol-relative input: keep the authority, supply only the scheme.
	if strings.HasPrefix(raw, "//") {
		return "https:" + raw
	}

	if u, err := url.Parse(raw); err == nil && u.Scheme != "" && !strings.Contains(u.Scheme, ".") {
		// Already carries a real scheme. Leave it alone so Validate can
		// accept it or reject it on its merits.
		return raw
	}

	return "https://" + raw
}

// Validate parses and checks a destination URL, returning the normalized
// form to store. The caller should store exactly what is returned here.
func (v *Validator) Validate(raw string) (string, error) {
	normalized := Normalize(raw)

	u, err := url.Parse(normalized)
	if err != nil {
		return "", fmt.Errorf("could not parse URL: %w", err)
	}

	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("%w (got %q)", ErrBadScheme, u.Scheme)
	}

	host := u.Hostname()
	if host == "" {
		return "", ErrNoHost
	}

	if v.isOwnHost(host) {
		return "", fmt.Errorf("%w: %s", ErrOwnDomain, host)
	}

	if err := v.checkAddresses(host); err != nil {
		return "", err
	}

	return normalized, nil
}

func (v *Validator) isOwnHost(host string) bool {
	host = strings.ToLower(host)
	for _, own := range v.ownHosts {
		if host == strings.ToLower(own) {
			return true
		}
	}
	return false
}

// checkAddresses resolves the host and rejects it if ANY resolved address is
// non-public. Any rather than all: a hostname with one public and one
// loopback A record must not be accepted.
//
// This is a creation-time check only. A host can be repointed at a private
// address after the link is created (DNS rebinding). Re-validating on every
// redirect would put a DNS lookup on the hottest path.
func (v *Validator) checkAddresses(host string) error {
	if ip := net.ParseIP(host); ip != nil {
		if !isPublic(ip) {
			return fmt.Errorf("%w: %s", ErrPrivateAddr, ip)
		}
		return nil
	}

	ips, err := v.resolve(host)
	if err != nil {
		return fmt.Errorf("%w: %s", ErrUnresolvable, host)
	}
	for _, ip := range ips {
		if !isPublic(ip) {
			return fmt.Errorf("%w: %s resolves to %s", ErrPrivateAddr, host, ip)
		}
	}
	return nil
}

func isPublic(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsUnspecified() || ip.IsMulticast() {
		return false
	}
	// Carrier-grade NAT (100.64.0.0/10) is not covered by IsPrivate but is
	// not a valid public destination either.
	if ip4 := ip.To4(); ip4 != nil && ip4[0] == 100 && ip4[1] >= 64 && ip4[1] <= 127 {
		return false
	}
	return true
}

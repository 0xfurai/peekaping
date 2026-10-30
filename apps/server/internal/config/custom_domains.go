package config

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

// NormalizeHost is a host name in the form it is compared in: trimmed, in lower case and without a
// trailing dot, which is how a resolver may write it.
func NormalizeHost(host string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
}

// ValidateCustomDomainCNAMETarget checks CUSTOM_DOMAIN_CNAME_TARGET: empty, or a host name. An IP
// address is not one, since a CNAME can't point at it. A mistyped value stops startup, as it
// would otherwise make every domain fail its check.
func ValidateCustomDomainCNAMETarget(target string) error {
	target = NormalizeHost(target)
	if target == "" {
		return nil
	}
	if !isHostName(target) {
		return fmt.Errorf("CUSTOM_DOMAIN_CNAME_TARGET must be a host name such as status.example.com, not %q", target)
	}
	return nil
}

// ValidateVerifyCustomDomains checks the raw text of VERIFY_CUSTOM_DOMAINS: empty (off), or one of
// the four values the shared boolean parser reads, true, 1, false and 0, exactly. The parser turns
// anything else, such as TRUE, yes or on, into false without an error, so a value meant to turn
// verification on would silently leave it off. The setting guards who may serve a domain, so
// startup stops instead.
func ValidateVerifyCustomDomains(raw string) error {
	switch raw {
	case "", "true", "1", "false", "0":
		return nil
	}
	return fmt.Errorf("VERIFY_CUSTOM_DOMAINS must be true, 1, false or 0, not %q: any other value would leave verification off", raw)
}

// isHostName reports whether s is a DNS name: dot-separated labels of letters, digits and hyphens,
// none starting or ending with a hyphen, and not an IP address.
func isHostName(s string) bool {
	if len(s) > 253 || net.ParseIP(s) != nil {
		return false
	}
	for _, label := range strings.Split(s, ".") {
		if !isHostLabel(label) {
			return false
		}
	}
	return true
}

func isHostLabel(label string) bool {
	if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
		return false
	}
	return strings.IndexFunc(label, notHostRune) < 0
}

func notHostRune(r rune) bool {
	return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-')
}

// CustomDomainCNAMETargetFor is the host a custom domain may be a CNAME to: the configured target,
// else the host of the public URL. It is empty when there is none: a public URL on localhost or at
// an IP address, which a CNAME can't point at, and then only the TXT record verifies a domain.
func CustomDomainCNAMETargetFor(configured, publicURL string) string {
	if target := NormalizeHost(configured); target != "" {
		return target
	}
	u, err := url.Parse(strings.TrimSpace(publicURL))
	if err != nil {
		return ""
	}
	host := NormalizeHost(u.Hostname())
	if host == "localhost" || !isHostName(host) {
		return ""
	}
	return host
}

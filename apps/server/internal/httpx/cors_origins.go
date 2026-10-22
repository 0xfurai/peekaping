package httpx

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// CORSOrigins is the set of origins whose pages may read the API's responses. It is the
// CORS_ORIGINS setting, parsed once at startup. The zero value and a nil *CORSOrigins allow
// no origin.
type CORSOrigins struct {
	any     bool
	origins map[string]struct{}
}

var (
	errNotOrigin = errors.New("want an origin such as https://example.com, with no path, query or credentials")
	errNotWebURL = errors.New("want an http or https URL such as https://example.com")
)

// ParseCORSOrigins parses a comma-separated list of origins, such as
// "https://peek.example,http://localhost:8383". An unset or empty list is the origin of
// clientURL. "*" alone allows any origin. An entry that isn't an origin, or a "*" next to other
// entries, is an error that names it.
func ParseCORSOrigins(list, clientURL string) (*CORSOrigins, error) {
	var entries []string
	for _, entry := range strings.Split(list, ",") {
		if entry = strings.TrimSpace(entry); entry != "" {
			entries = append(entries, entry)
		}
	}

	if len(entries) == 0 {
		origin, err := clientOrigin(clientURL)
		if err != nil {
			return nil, fmt.Errorf("invalid CLIENT_URL %q: %w", clientURL, err)
		}
		return &CORSOrigins{origins: map[string]struct{}{origin: {}}}, nil
	}

	origins := &CORSOrigins{origins: map[string]struct{}{}}
	for _, entry := range entries {
		if entry == "*" {
			if len(entries) > 1 {
				return nil, fmt.Errorf("invalid CORS_ORIGINS entry %q: it allows any origin, so it can't be listed with others", entry)
			}
			origins.any = true
			continue
		}
		origin, err := parseOrigin(entry)
		if err != nil {
			return nil, fmt.Errorf("invalid CORS_ORIGINS entry %q: %w", entry, err)
		}
		origins.origins[origin] = struct{}{}
	}
	return origins, nil
}

// parseOrigin normalizes a list entry: the scheme and host in lowercase, the port as given and a
// trailing slash dropped. That is the form a browser sends in the Origin header.
func parseOrigin(entry string) (string, error) {
	u, err := url.Parse(entry)
	if err != nil || u.Opaque != "" || u.User != nil || strings.ContainsAny(entry, "?#") ||
		(u.Path != "" && u.Path != "/") {
		return "", errNotOrigin
	}
	origin, ok := originOf(u)
	if !ok {
		return "", errNotOrigin
	}
	return origin, nil
}

// clientOrigin is the origin of CLIENT_URL, whatever path it has.
func clientOrigin(clientURL string) (string, error) {
	u, err := url.Parse(clientURL)
	if err != nil {
		return "", errNotWebURL
	}
	origin, ok := originOf(u)
	if !ok {
		return "", errNotWebURL
	}
	return origin, nil
}

func originOf(u *url.URL) (origin string, ok bool) {
	scheme := strings.ToLower(u.Scheme)
	if (scheme != "http" && scheme != "https") || u.Hostname() == "" {
		return "", false
	}
	return scheme + "://" + strings.ToLower(u.Host), true
}

// AllowOrigin returns the Access-Control-Allow-Origin value for a request from origin, or ""
// when a page on origin may not read the response.
func (o *CORSOrigins) AllowOrigin(origin string) string {
	if o == nil {
		return ""
	}
	if o.any {
		return "*"
	}
	if _, ok := o.origins[origin]; ok {
		return origin
	}
	return ""
}

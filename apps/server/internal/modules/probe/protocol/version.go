package protocol

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/proxy"
)

// versionHeader starts the text that a version is the hash of. It is the version of that text's
// format: a change to the format has to change it.
const versionHeader = "peekaping.probe.assignments.v1\n"

// versionBytes is how many bytes of the SHA-256 the version keeps: 128 bits, which no two
// different assignments will share
const versionBytes = 16

// Version names the assignment of a probe: the set of monitors it can be dealt, the proxies they
// use, the monitor types the probe supports, and the instances it is dealt among. It changes when
// any of them does, and a probe that holds one and sees a different one knows to fetch its
// monitors again.
//
// It is the base64url of the first 16 bytes of the SHA-256 of this text, which is built from the
// arguments alone, so that every API replica that reads the same rows computes the same version:
//
//	peekaping.probe.assignments.v1
//	monitor <id> <updated_at> <proxy_id> <proxy updated_at>   one for each dealable monitor, by id
//	type <monitor type>                                       one for each supported type, sorted
//	instance <instance id>                                    one for each online instance, sorted
//
// The ids and types are Go-quoted, so that no value can run into the next. The times are in UTC
// with RFC 3339 and nanoseconds, and a monitor with no proxy, or one whose proxy is not in proxies
// (it was deleted), has - for the proxy's time. A dealable monitor is one of Dealable. No map's
// order matters, and a type or an instance listed twice counts once.
//
// It covers every dealable monitor of the probe and not the share of one instance, so the
// instances of a probe all hold the same version, and a change anywhere in it wakes them all.
func Version(monitors []*monitor.Model, proxies map[string]*proxy.Model, supportedTypes []string, instanceIDs []string) string {
	dealable := Dealable(monitors, supportedTypes)
	slices.SortFunc(dealable, func(a, b *monitor.Model) int { return strings.Compare(a.ID, b.ID) })

	h := sha256.New()
	_, _ = io.WriteString(h, versionHeader)
	for _, m := range dealable {
		proxyUpdatedAt := "-"
		if p := proxies[m.ProxyId]; m.ProxyId != "" && p != nil {
			proxyUpdatedAt = versionTime(p.UpdatedAt)
		}
		_, _ = fmt.Fprintf(h, "monitor %q %s %q %s\n", m.ID, versionTime(m.UpdatedAt), m.ProxyId, proxyUpdatedAt)
	}
	for _, monitorType := range sortedSet(supportedTypes) {
		_, _ = fmt.Fprintf(h, "type %q\n", monitorType)
	}
	for _, instanceID := range sortedSet(instanceIDs) {
		_, _ = fmt.Fprintf(h, "instance %q\n", instanceID)
	}
	return base64.RawURLEncoding.EncodeToString(h.Sum(nil)[:versionBytes])
}

// versionTime is how a version writes a moment: the same text for the same instant, in whatever
// zone the database driver gave it
func versionTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}

// sortedSet returns the distinct values of s, sorted, and leaves s as it is
func sortedSet(s []string) []string {
	sorted := slices.Clone(s)
	slices.Sort(sorted)
	return slices.Compact(sorted)
}

package protocol

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"slices"
	"strings"

	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/probe"
	"peekaping/internal/modules/proxy"
)

// versionHeader starts the text that a version is the hash of. It is the version of that text's
// format: a change to the format has to change it.
const versionHeader = "peekaping.probe.assignments.v2\n"

// versionBytes is how many bytes of the SHA-256 the version keeps: 128 bits, which no two
// different assignments will share
const versionBytes = 16

// Version names the assignment of a probe: the set of monitors it can be dealt, the proxies they
// use, the monitor types its online instances run, and the instances it is dealt among with the
// types each runs. It changes when any of them does, and a probe that holds one and sees a
// different one knows to fetch its monitors again.
//
// It is the base64url of the first 16 bytes of the SHA-256 of this text, which is built from the
// arguments alone, so that every API replica that reads the same rows computes the same version:
//
//	peekaping.probe.assignments.v2
//	monitor <id> <name> <type> <interval> <timeout> <max retries> <retry interval> <resend interval> <config>
//	        <proxy id> <proxy protocol> <proxy host> <proxy port> <proxy auth> <proxy username> <proxy password>
//	                                                        one line for each dealable monitor, by id
//	type <monitor type>                                     one for each type the instances run between them, sorted
//	instance <instance id>                                  one for each online instance, sorted
//	instance_type <instance id> <monitor type>              one for each type of an instance that does not run the
//	                                                        types the instances run between them, by instance, sorted
//
// A monitor line is what the probe is told about the monitor and its proxy, and nothing else. The
// monitor's updated_at is not in it, nor is its proxy's: a monitor's row is touched when its status
// changes, which the probe has no use for, and a version that moved with every status change would
// make the probe fetch and restart its monitors each time. The strings are Go-quoted, so that no
// value can run into the next. A monitor with no proxy, or one whose proxy is not in proxies (it
// was deleted), has the proxy's fields empty, as it is told without one. A dealable monitor is one
// of Dealable over the types the instances run between them (probe.UnionTypes). No map's order
// matters, and a type or an instance listed twice counts once.
//
// The instance_type lines are there for instances of one probe on different builds, which are dealt
// the monitors they run and no others: the types between them and the ids of the instances don't
// say which monitor goes to which. An instance that runs the types between them, as every instance
// of a probe on one build does, has none, so that the version of such a probe is the one it had
// before instances listed types of their own, and a server upgrade makes none of them fetch their
// monitors again.
//
// It covers every dealable monitor of the probe and not the share of one instance, so the
// instances of a probe all hold the same version, and a change anywhere in it wakes them all.
func Version(monitors []*monitor.Model, proxies map[string]*proxy.Model, instances []*probe.Instance) string {
	supportedTypes := probe.UnionTypes(instances)
	dealable := Dealable(monitors, supportedTypes)
	slices.SortFunc(dealable, func(a, b *monitor.Model) int { return strings.Compare(a.ID, b.ID) })
	sorted := slices.Clone(instances)
	slices.SortFunc(sorted, func(a, b *probe.Instance) int { return strings.Compare(a.ID, b.ID) })
	sorted = slices.CompactFunc(sorted, func(a, b *probe.Instance) bool { return a.ID == b.ID })

	h := sha256.New()
	_, _ = io.WriteString(h, versionHeader)
	for _, m := range dealable {
		var p proxy.Model
		if found := proxies[m.ProxyId]; m.ProxyId != "" && found != nil {
			p = *found
		}
		_, _ = fmt.Fprintf(h, "monitor %q %q %q %d %d %d %d %d %q %q %q %q %d %t %q %q\n",
			m.ID, m.Name, m.Type, m.Interval, m.Timeout, m.MaxRetries, m.RetryInterval, m.ResendInterval, m.Config,
			p.ID, p.Protocol, p.Host, p.Port, p.Auth, p.Username, p.Password)
	}
	between := sortedSet(supportedTypes)
	for _, monitorType := range between {
		_, _ = fmt.Fprintf(h, "type %q\n", monitorType)
	}
	for _, instance := range sorted {
		_, _ = fmt.Fprintf(h, "instance %q\n", instance.ID)
	}
	for _, instance := range sorted {
		own := sortedSet(instance.SupportedTypes)
		if slices.Equal(own, between) {
			continue
		}
		for _, monitorType := range own {
			_, _ = fmt.Fprintf(h, "instance_type %q %q\n", instance.ID, monitorType)
		}
	}
	return base64.RawURLEncoding.EncodeToString(h.Sum(nil)[:versionBytes])
}

// sortedSet returns the distinct values of s, sorted, and leaves s as it is
func sortedSet(s []string) []string {
	sorted := slices.Clone(s)
	slices.Sort(sorted)
	return slices.Compact(sorted)
}

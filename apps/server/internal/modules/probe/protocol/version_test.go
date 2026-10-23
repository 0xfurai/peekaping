package protocol_test

import (
	"math/rand/v2"
	"slices"
	"strconv"
	"testing"
	"time"

	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/probe/protocol"
	"peekaping/internal/modules/proxy"

	"github.com/stretchr/testify/assert"
)

// assignment is everything a version is made of
type assignment struct {
	monitors  []*monitor.Model
	proxies   map[string]*proxy.Model
	supported []string
	instances []string
}

func (a assignment) version() string {
	return protocol.Version(a.monitors, a.proxies, a.supported, a.instances)
}

var epoch = time.Date(2026, 10, 1, 8, 0, 0, 123456789, time.UTC)

// baseAssignment is a probe with an http monitor behind a proxy, a tcp monitor, a push monitor
// that is never dealt, a paused monitor, and two instances
func baseAssignment() assignment {
	http := monitorWith("monitor-http", "http")
	http.Interval, http.Timeout, http.MaxRetries, http.RetryInterval, http.ResendInterval = 60, 16, 3, 30, 10
	http.Config = `{"url":"https://example.com/health"}`
	http.ProxyId = "proxy-1"
	http.UpdatedAt = epoch
	tcp := monitorWith("monitor-tcp", "tcp")
	tcp.Interval, tcp.Timeout = 120, 10
	tcp.Config = `{"host":"db.example.com","port":5432}`
	tcp.UpdatedAt = epoch.Add(time.Minute)
	push := monitorWith("monitor-push", "push")
	push.UpdatedAt = epoch.Add(2 * time.Minute)
	paused := monitorWith("monitor-paused", "tcp")
	paused.Active = false
	paused.UpdatedAt = epoch.Add(4 * time.Minute)
	return assignment{
		monitors: []*monitor.Model{http, tcp, push, paused},
		proxies: map[string]*proxy.Model{"proxy-1": {
			ID: "proxy-1", Protocol: "socks5", Host: "proxy.example.com", Port: 3128, Auth: true, Username: "scout", Password: "s3cret",
			UpdatedAt: epoch.Add(3 * time.Minute),
		}},
		supported: []string{"http", "tcp", "push"},
		instances: []string{"instance-a", "instance-b"},
	}
}

func TestVersion_ChangesOn(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(a *assignment)
	}{
		{"a monitor is added", func(a *assignment) {
			a.monitors = append(a.monitors, monitorWith("monitor-dns", "dns"))
			a.supported = append(a.supported, "dns")
		}},
		{"a monitor is removed", func(a *assignment) { a.monitors = a.monitors[1:] }},
		{"a monitor's config changed", func(a *assignment) { a.monitors[1].Config = `{"host":"db.example.com","port":5433}` }},
		{"a monitor's interval changed", func(a *assignment) { a.monitors[1].Interval = 180 }},
		{"a monitor's timeout changed", func(a *assignment) { a.monitors[1].Timeout = 12 }},
		{"a monitor's max retries changed", func(a *assignment) { a.monitors[1].MaxRetries = 1 }},
		{"a monitor's retry interval changed", func(a *assignment) { a.monitors[1].RetryInterval = 45 }},
		{"a monitor's resend interval changed", func(a *assignment) { a.monitors[1].ResendInterval = 5 }},
		{"a monitor's name changed", func(a *assignment) { a.monitors[1].Name = "Database" }},
		{"a monitor is paused", func(a *assignment) { a.monitors[1].Active = false }},
		{"a monitor is resumed", func(a *assignment) { a.monitors[3].Active = true }},
		{"a monitor changes to a type the probe can't run", func(a *assignment) { a.monitors[1].Type = "dns" }},
		{"a proxy's host changed", func(a *assignment) { a.proxies["proxy-1"].Host = "egress.example.com" }},
		{"a proxy's port changed", func(a *assignment) { a.proxies["proxy-1"].Port = 1080 }},
		{"a proxy's protocol changed", func(a *assignment) { a.proxies["proxy-1"].Protocol = "http" }},
		{"a proxy's auth changed", func(a *assignment) { a.proxies["proxy-1"].Auth = false }},
		{"a proxy's username changed", func(a *assignment) { a.proxies["proxy-1"].Username = "ranger" }},
		{"a proxy's password changed", func(a *assignment) { a.proxies["proxy-1"].Password = "n3w-s3cret" }},
		{"a monitor changes to another proxy", func(a *assignment) {
			a.monitors[0].ProxyId = "proxy-2"
			a.proxies["proxy-2"] = &proxy.Model{ID: "proxy-2", Protocol: "socks5", Host: "proxy.example.com", Port: 3128, Auth: true, Username: "scout", Password: "s3cret"}
		}},
		{"a monitor's proxy is deleted", func(a *assignment) {
			a.monitors[0].ProxyId = ""
			delete(a.proxies, "proxy-1")
		}},
		{"a monitor gets a proxy", func(a *assignment) {
			a.monitors[1].ProxyId = "proxy-1"
		}},
		{"the supported types change", func(a *assignment) { a.supported = []string{"http", "push"} }},
		{"the supported types are no longer listed", func(a *assignment) { a.supported = nil }},
		{"an instance joins", func(a *assignment) { a.instances = append(a.instances, "instance-c") }},
		{"an instance leaves", func(a *assignment) { a.instances = a.instances[:1] }},
		{"an instance is replaced", func(a *assignment) { a.instances[1] = "instance-d" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := baseAssignment()
			after := baseAssignment()
			tc.change(&after)

			assert.NotEqual(t, before.version(), after.version())
		})
	}
}

func TestVersion_SameInputsSameVersion(t *testing.T) {
	build := func() assignment {
		a := baseAssignment()
		for i := range 30 {
			id := "proxy-" + strconv.Itoa(i+10)
			a.proxies[id] = &proxy.Model{ID: id, Host: id + ".example.com", Port: 3128 + i}
			m := monitorWith("extra-"+strconv.Itoa(i), "http")
			m.ProxyId = id
			m.UpdatedAt = epoch.Add(time.Duration(i) * time.Minute)
			a.monitors = append(a.monitors, m)
		}
		return a
	}
	want := build().version()
	rng := rand.New(rand.NewPCG(1, 2))

	for range 50 {
		shuffled := build()
		rng.Shuffle(len(shuffled.monitors), func(i, j int) {
			shuffled.monitors[i], shuffled.monitors[j] = shuffled.monitors[j], shuffled.monitors[i]
		})
		rng.Shuffle(len(shuffled.instances), func(i, j int) {
			shuffled.instances[i], shuffled.instances[j] = shuffled.instances[j], shuffled.instances[i]
		})
		rng.Shuffle(len(shuffled.supported), func(i, j int) {
			shuffled.supported[i], shuffled.supported[j] = shuffled.supported[j], shuffled.supported[i]
		})

		assert.Equal(t, want, shuffled.version())
	}
}

// A monitor's row is touched whenever its status changes, which the probe is not told, so a
// version that followed updated_at would change with every Up and Down and make the probe restart
// its monitors each time.
func TestVersion_IgnoresUpdatedAt(t *testing.T) {
	before := baseAssignment()
	after := baseAssignment()
	for _, m := range after.monitors {
		m.UpdatedAt = m.UpdatedAt.Add(time.Hour)
	}
	after.monitors[0].Status = 1
	after.proxies["proxy-1"].UpdatedAt = after.proxies["proxy-1"].UpdatedAt.Add(time.Hour)

	assert.Equal(t, before.version(), after.version())

	zeroed := baseAssignment()
	for _, m := range zeroed.monitors {
		m.UpdatedAt = time.Time{}
	}
	zeroed.proxies["proxy-1"].UpdatedAt = time.Time{}

	assert.Equal(t, before.version(), zeroed.version())
}

func TestVersion_IgnoresWhatTheProbeIsNotDealt(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(a *assignment)
	}{
		{"a push monitor is updated", func(a *assignment) { a.monitors[2].UpdatedAt = a.monitors[2].UpdatedAt.Add(time.Hour) }},
		{"a push monitor is added", func(a *assignment) { a.monitors = append(a.monitors, monitorWith("monitor-push-2", "push")) }},
		{"a push monitor is removed", func(a *assignment) { a.monitors = slices.Delete(a.monitors, 2, 3) }},
		{"a push monitor behind a proxy", func(a *assignment) { a.monitors[2].ProxyId = "proxy-1" }},
		{"a paused monitor is updated", func(a *assignment) { a.monitors[3].UpdatedAt = a.monitors[3].UpdatedAt.Add(time.Hour) }},
		{"a paused monitor is deleted", func(a *assignment) { a.monitors = a.monitors[:3] }},
		{"a monitor of a type the probe can't run is added", func(a *assignment) { a.monitors = append(a.monitors, monitorWith("monitor-dns", "dns")) }},
		{"a proxy no monitor uses changes", func(a *assignment) {
			a.proxies["proxy-unused"] = &proxy.Model{ID: "proxy-unused", UpdatedAt: epoch}
		}},
		{"a supported type is listed twice", func(a *assignment) { a.supported = append(a.supported, "http") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := baseAssignment()
			after := baseAssignment()
			tc.change(&after)

			assert.Equal(t, before.version(), after.version())
		})
	}
}

func TestVersion_IgnoresPushMonitors(t *testing.T) {
	withoutPush := baseAssignment()
	withoutPush.monitors = slices.Delete(withoutPush.monitors, 2, 3)

	assert.Equal(t, withoutPush.version(), baseAssignment().version())
}

func TestVersion_WithNothingIsStillAVersion(t *testing.T) {
	empty := protocol.Version(nil, nil, nil, nil)

	assert.NotEmpty(t, empty)
	assert.NotEqual(t, empty, baseAssignment().version())
	assert.Equal(t, empty, protocol.Version([]*monitor.Model{}, map[string]*proxy.Model{}, []string{}, []string{}))
}

// A version is a query parameter, which the probe sends back untouched
func TestVersion_IsSafeInAQueryString(t *testing.T) {
	v := baseAssignment().version()

	assert.Regexp(t, `^[A-Za-z0-9_-]{22}$`, v)
}

// The replicas of an API compute the version each from the database, and a rolling upgrade
// runs two releases at once. They agree only if the version of the same rows never changes, so a
// change to this literal is a change to the protocol.
func TestVersion_IsTheSameAcrossReleases(t *testing.T) {
	assert.Equal(t, "DHjgfz_7nvvgT_tLMYKJJw", baseAssignment().version())
}

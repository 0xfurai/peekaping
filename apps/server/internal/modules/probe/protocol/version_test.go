package protocol_test

import (
	"math/rand/v2"
	"slices"
	"strconv"
	"testing"
	"time"

	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/probe"
	"peekaping/internal/modules/probe/protocol"
	"peekaping/internal/modules/proxy"

	"github.com/stretchr/testify/assert"
)

// assignment is everything a version is made of
type assignment struct {
	monitors  []*monitor.Model
	proxies   map[string]*proxy.Model
	instances []*probe.Instance
}

func (a assignment) version() string {
	return protocol.Version(a.monitors, a.proxies, a.instances)
}

// everyoneRuns has every instance list the types
func (a *assignment) everyoneRuns(types ...string) {
	for _, instance := range a.instances {
		instance.SupportedTypes = slices.Clone(types)
	}
}

var epoch = time.Date(2026, 10, 1, 8, 0, 0, 123456789, time.UTC)

// baseAssignment is a probe with an http monitor behind a proxy, a tcp monitor, a push monitor
// that is never dealt, a paused monitor, and two instances that run the same types, as the instances
// of a probe on one build do
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
		instances: []*probe.Instance{
			{ID: "instance-a", SupportedTypes: []string{"http", "tcp", "push"}},
			{ID: "instance-b", SupportedTypes: []string{"http", "tcp", "push"}},
		},
	}
}

func TestVersion_ChangesOn(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(a *assignment)
	}{
		{"a monitor is added", func(a *assignment) {
			a.monitors = append(a.monitors, monitorWith("monitor-dns", "dns"))
			a.everyoneRuns("http", "tcp", "push", "dns")
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
		{"the supported types change", func(a *assignment) { a.everyoneRuns("http", "push") }},
		{"the supported types are no longer listed", func(a *assignment) { a.everyoneRuns() }},
		{"an instance joins", func(a *assignment) {
			a.instances = append(a.instances, &probe.Instance{ID: "instance-c", SupportedTypes: []string{"http", "tcp", "push"}})
		}},
		{"an instance leaves", func(a *assignment) { a.instances = a.instances[:1] }},
		{"an instance is replaced", func(a *assignment) { a.instances[1].ID = "instance-d" }},
		// Same instances and the same types between them, but not the same monitors for each
		{"an instance stops running a type another still runs", func(a *assignment) { a.instances[1].SupportedTypes = []string{"http", "push"} }},
		{"an instance that listed no types lists them", func(a *assignment) { a.instances[1].SupportedTypes = nil }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := baseAssignment()
			after := baseAssignment()
			tc.change(&after)

			assert.NotEqual(t, before.version(), after.version())
		})
	}
}

// The union of the types and the ids of the instances are the same, but each monitor is dealt to
// the other instance now
func TestVersion_ChangesWhenInstancesSwapTheTypesTheyRun(t *testing.T) {
	before := baseAssignment()
	before.instances[0].SupportedTypes, before.instances[1].SupportedTypes = []string{"http", "push"}, []string{"tcp", "push"}
	after := baseAssignment()
	after.instances[0].SupportedTypes, after.instances[1].SupportedTypes = []string{"tcp", "push"}, []string{"http", "push"}

	assert.NotEqual(t, before.version(), after.version())
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
		for _, instance := range shuffled.instances {
			rng.Shuffle(len(instance.SupportedTypes), func(i, j int) {
				instance.SupportedTypes[i], instance.SupportedTypes[j] = instance.SupportedTypes[j], instance.SupportedTypes[i]
			})
		}

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
		{"a supported type is listed twice", func(a *assignment) {
			a.instances[0].SupportedTypes = append(a.instances[0].SupportedTypes, "http")
		}},
		{"an instance lists the types in another order", func(a *assignment) { a.instances[1].SupportedTypes = []string{"tcp", "push", "http"} }},
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
	empty := protocol.Version(nil, nil, nil)

	assert.NotEmpty(t, empty)
	assert.NotEqual(t, empty, baseAssignment().version())
	assert.Equal(t, empty, protocol.Version([]*monitor.Model{}, map[string]*proxy.Model{}, []*probe.Instance{}))
}

// A version is a query parameter, which the probe sends back untouched
func TestVersion_IsSafeInAQueryString(t *testing.T) {
	v := baseAssignment().version()

	assert.Regexp(t, `^[A-Za-z0-9_-]{22}$`, v)
}

// The replicas of an API compute the version each from the database, and a rolling upgrade
// runs two releases at once. They agree only if the version of the same rows never changes, so a
// change to this literal is a change to the protocol. The instances here run the same types, as
// those of a probe on one build do: instances that list types of their own (#264) are the one
// thing that adds to the text of the version, and a probe on one build must not see a new version
// when its server is upgraded.
func TestVersion_IsTheSameAcrossReleases(t *testing.T) {
	assert.Equal(t, "DHjgfz_7nvvgT_tLMYKJJw", baseAssignment().version())
}

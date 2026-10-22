package protocol_test

import (
	"strconv"
	"testing"

	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/probe/protocol"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

// monitorWith is an active monitor of the given type
func monitorWith(id, monitorType string) *monitor.Model {
	return &monitor.Model{ID: id, Name: "monitor " + id, Type: monitorType, Active: true}
}

// manyMonitors are n active http monitors with the ids the API would give them: random-looking
// UUIDs, which the test fixes so that it deals the same ones every run
func manyMonitors(n int) []*monitor.Model {
	monitors := make([]*monitor.Model, n)
	for i := range monitors {
		monitors[i] = monitorWith(uuid.NewSHA1(uuid.NameSpaceOID, []byte(strconv.Itoa(i))).String(), "http")
	}
	return monitors
}

// instanceIDs are the ids a probe process chooses: random UUIDs
func instanceIDs(n int) []string {
	ids := make([]string, n)
	for i := range ids {
		ids[i] = uuid.NewSHA1(uuid.NameSpaceDNS, []byte("instance-"+strconv.Itoa(i))).String()
	}
	return ids
}

func idsOf(monitors []*monitor.Model) []string {
	ids := make([]string, len(monitors))
	for i, m := range monitors {
		ids[i] = m.ID
	}
	return ids
}

func TestDeal_OneInstanceGetsAll(t *testing.T) {
	monitors := manyMonitors(40)

	got := protocol.Deal(monitors, []string{"only"}, "only")

	assert.ElementsMatch(t, idsOf(monitors), idsOf(got))
}

func TestDeal_NoInstancesGetNothing(t *testing.T) {
	monitors := manyMonitors(10)

	assert.Empty(t, protocol.Deal(monitors, nil, "stranger"))
	assert.Empty(t, protocol.Deal(monitors, []string{"a", "b"}, "stranger"), "an instance that isn't among them has no share")
}

func TestDeal_TwoAndThreeInstancesSplitEveryMonitorOnce(t *testing.T) {
	monitors := manyMonitors(300)

	for name, ids := range map[string][]string{
		"uuid instance ids":                      instanceIDs(3),
		"instance ids that differ in their last": {"edge-1", "edge-2", "edge-3"},
	} {
		for _, count := range []int{2, 3} {
			t.Run(name+" "+strconv.Itoa(count)+" instances", func(t *testing.T) {
				instances := ids[:count]
				owners := map[string]string{}
				for _, instance := range instances {
					share := protocol.Deal(monitors, instances, instance)

					assert.GreaterOrEqual(t, len(share)*5, len(monitors), "%s has at least 20%% of the monitors, has %d of %d", instance, len(share), len(monitors))
					for _, m := range share {
						assert.NotContains(t, owners, m.ID, "%s is dealt to two instances", m.ID)
						owners[m.ID] = instance
					}
				}
				assert.Len(t, owners, len(monitors), "every monitor is on one of the instances")
			})
		}
	}
}

// An instance id is whatever the probe chose, and a person may choose i1, i2, i3 and i4. Hashing
// them with FNV-1a alone gave one of the four half of the monitors.
func TestDeal_ShortInstanceIdsStillSplitEvenly(t *testing.T) {
	monitors := manyMonitors(1000)
	instances := []string{"i1", "i2", "i3", "i4"}

	for _, instance := range instances {
		share := protocol.Deal(monitors, instances, instance)

		assert.GreaterOrEqual(t, len(share)*5, len(monitors), "%s has at least 20%% of the monitors, has %d of %d", instance, len(share), len(monitors))
	}
}

func TestDeal_DoesNotDependOnTheOrderOfTheInstances(t *testing.T) {
	monitors := manyMonitors(50)
	ids := instanceIDs(3)

	forward := protocol.Deal(monitors, ids, ids[1])
	backward := protocol.Deal(monitors, []string{ids[2], ids[1], ids[0]}, ids[1])

	assert.ElementsMatch(t, idsOf(forward), idsOf(backward))
}

func TestDeal_InstanceLeavingMovesOnlyItsShare(t *testing.T) {
	monitors := manyMonitors(300)
	ids := instanceIDs(3)
	before := map[string][]string{}
	for _, id := range ids {
		before[id] = idsOf(protocol.Deal(monitors, ids, id))
	}
	leaving, staying := ids[2], ids[:2]

	for _, id := range staying {
		after := idsOf(protocol.Deal(monitors, staying, id))

		assert.Subset(t, after, before[id], "%s keeps every monitor it had", id)
		assert.Greater(t, len(after), len(before[id]), "and takes some of the leaving instance's")
	}
	taken := len(idsOf(protocol.Deal(monitors, staying, staying[0]))) + len(idsOf(protocol.Deal(monitors, staying, staying[1])))
	assert.Equal(t, len(monitors), taken, "the two staying instances hold all of them between them")
	assert.NotEmpty(t, before[leaving])
}

func TestDeal_SkipsPushAndUnsupportedTypes(t *testing.T) {
	paused := monitorWith("paused", "http")
	paused.Active = false
	monitors := []*monitor.Model{
		monitorWith("http-1", "http"),
		monitorWith("tcp-1", "tcp"),
		monitorWith("push-1", "push"),
		monitorWith("dns-1", "dns"),
		paused,
	}

	got := protocol.Dealable(monitors, []string{"http", "tcp", "push"})

	assert.Equal(t, []string{"http-1", "tcp-1"}, idsOf(got), "no push monitor, even for a probe that lists push; no type it can't run; no paused monitor")
}

func TestDeal_EmptySupportedTypesMeansEveryType(t *testing.T) {
	paused := monitorWith("paused", "http")
	paused.Active = false
	monitors := []*monitor.Model{
		monitorWith("http-1", "http"),
		monitorWith("tcp-1", "tcp"),
		monitorWith("push-1", "push"),
		paused,
	}

	assert.Equal(t, []string{"http-1", "tcp-1"}, idsOf(protocol.Dealable(monitors, nil)))
	assert.Equal(t, []string{"http-1", "tcp-1"}, idsOf(protocol.Dealable(monitors, []string{})))
}

func TestDealable_ReturnsNoMonitorsAsAnEmptyList(t *testing.T) {
	assert.NotNil(t, protocol.Dealable(nil, nil))
	assert.NotNil(t, protocol.Deal(nil, []string{"a"}, "a"))
}

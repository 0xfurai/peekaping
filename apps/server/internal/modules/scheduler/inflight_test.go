package scheduler

import (
	"context"
	"strconv"
	"testing"
	"time"

	"peekaping/internal/modules/healthcheck/executor"

	"github.com/stretchr/testify/assert"
)

func TestScheduler_SkipsACheckWhileTheMonitorsPreviousCheckIsStillRunning(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.db.put(monitorWith("mon", withInterval(1)))
	release := make(chan struct{})
	h.exec.run = func(_ context.Context, call checkCall) *executor.Result {
		if call.n == 1 {
			<-release
		}
		return up("check " + strconv.Itoa(call.n))
	}
	h.start()
	eventually(t, 2*time.Second, func() bool { return h.exec.countFor("mon") == 1 }, "the first check is running")

	time.Sleep(2300 * time.Millisecond) // the timer fires at least twice
	assert.Equal(t, 1, h.exec.countFor("mon"), "a monitor runs one check at a time: the checks that fell due are skipped")

	close(release)
	eventually(t, 2*time.Second, func() bool { return h.exec.countFor("mon") >= 2 }, "the monitor is checked again once its check is done")
	assert.Equal(t, "check 1", h.ingest.messages()[0], "the check that was running isn't lost")
}

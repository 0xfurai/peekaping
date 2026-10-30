package executor

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// hasMonotonicReading reports whether t still carries Go's monotonic clock reading, which
// String prints as "m=+0.001". Truncating, rounding and UTC() all drop it.
func hasMonotonicReading(t time.Time) bool {
	return strings.Contains(t.String(), "m=")
}

func TestExecutor_ResultTimesKeepTheirMonotonicReading(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer l.Close()

	result := NewTCPExecutor(zap.NewNop().Sugar()).Execute(context.Background(), tcpMonitor(t, l.Addr().String()), nil)

	require.NotNil(t, result)
	assert.True(t, hasMonotonicReading(result.StartTime), "start time %v has no monotonic reading", result.StartTime)
	assert.True(t, hasMonotonicReading(result.EndTime), "end time %v has no monotonic reading", result.EndTime)
}

func TestExecutor_ResultOfAFailedCheckKeepsItsMonotonicReading(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := l.Addr().String()
	require.NoError(t, l.Close())

	result := NewTCPExecutor(zap.NewNop().Sugar()).Execute(context.Background(), tcpMonitor(t, addr), nil)

	require.NotNil(t, result)
	assert.True(t, hasMonotonicReading(result.StartTime), "start time %v has no monotonic reading", result.StartTime)
	assert.True(t, hasMonotonicReading(result.EndTime), "end time %v has no monotonic reading", result.EndTime)
}

// A check's times are read as time.Now(). Calling UTC() on them drops the monotonic reading, so a
// step of the wall clock during a check would change the response time, even to a negative one.
// The supervisor converts a result's times to UTC when the result leaves it.
func TestExecutorSources_DoNotStripTheMonotonicReadingFromACheckTime(t *testing.T) {
	files, err := filepath.Glob("*.go")
	require.NoError(t, err)
	require.NotEmpty(t, files)

	// allowed lists the files that may read time.Now().UTC(): none
	allowed := map[string]bool{}

	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") || allowed[file] {
			continue
		}
		source, err := os.ReadFile(file)
		require.NoError(t, err)
		for i, line := range strings.Split(string(source), "\n") {
			if strings.Contains(line, "time.Now().UTC()") {
				t.Errorf("%s:%d reads time.Now().UTC(), which drops the monotonic reading: %s", file, i+1, strings.TrimSpace(line))
			}
		}
	}
}

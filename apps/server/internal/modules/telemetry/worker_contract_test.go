package telemetry

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"testing"

	"peekaping/internal/config"
	"peekaping/internal/modules/maintenance"
	"peekaping/internal/modules/status_page"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The Worker in apps/telemetry accepts only the payload its validator describes. These tests read
// the validator's rules from its source, so that a change on either side fails here instead of
// failing quietly in production, where the Worker answers 400 and the server drops the ping.

func workerValidatorSource(t *testing.T) string {
	t.Helper()
	source, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "telemetry", "src", "validate.ts"))
	if errors.Is(err, fs.ErrNotExist) {
		t.Skip("apps/telemetry is not in this checkout")
	}
	require.NoError(t, err)
	return string(source)
}

// workerPattern is the regular expression the validator names, such as TYPE_NAME
func workerPattern(t *testing.T, source, name string) *regexp.Regexp {
	t.Helper()
	found := regexp.MustCompile(`(?m)^const ` + name + ` = /(.+)/;$`).FindStringSubmatch(source)
	require.NotNil(t, found, "no regular expression named %s in validate.ts", name)
	return regexp.MustCompile(found[1])
}

// workerList is the list of strings the validator names, such as MONITOR_BUCKETS
func workerList(t *testing.T, source, name string) []string {
	t.Helper()
	found := regexp.MustCompile(`(?s)const ` + name + ` = \[(.*?)\] as const;`).FindStringSubmatch(source)
	require.NotNil(t, found, "no list named %s in validate.ts", name)
	var items []string
	for _, quoted := range regexp.MustCompile(`"([^"]*)"`).FindAllStringSubmatch(found[1], -1) {
		items = append(items, quoted[1])
	}
	return items
}

func workerNumber(t *testing.T, source, name string) int {
	t.Helper()
	found := regexp.MustCompile(`const ` + name + ` = (\d+);`).FindStringSubmatch(source)
	require.NotNil(t, found, "no number named %s in validate.ts", name)
	number, err := strconv.Atoi(found[1])
	require.NoError(t, err)
	return number
}

func keysOf(t *testing.T, encoded []byte) []string {
	t.Helper()
	var object map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(encoded, &object))
	keys := make([]string, 0, len(object))
	for key := range object {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func sorted(items []string) []string {
	out := append([]string(nil), items...)
	sort.Strings(out)
	return out
}

func TestBuildPayload_PassesTheWorkersRules(t *testing.T) {
	source := workerValidatorSource(t)
	uuidV4 := workerPattern(t, source, "UUID_V4")
	versionRule := workerPattern(t, source, "VERSION")
	osRule := workerPattern(t, source, "OS")
	typeRule := workerPattern(t, source, "TYPE_NAME")

	for _, mode := range []Mode{ModeSingle, ModeCluster} {
		for _, dbType := range []string{"sqlite", "postgres", "postgresql", "mysql"} {
			t.Run(string(mode)+"/"+dbType, func(t *testing.T) {
				in := newInstall()
				in.mode = mode
				in.cfg = &config.Config{DBType: dbType, MetricsEnabled: true}
				in.optIn("0d2f6a9e-3b1c-4e7a-8c5d-9f0a1b2c3d4e")
				in.monitors.items = monitorsOfType("http", "tcp", "grpc-keyword", "kafka-producer", "sqlserver")
				in.channels.items = channelsOfType("slack", "google_chat", "webhook")
				in.statusPages.items = []*status_page.Model{{ID: "1"}}
				in.maintenances.items = []*maintenance.Model{{ID: "1"}}

				payload, err := in.service().BuildPayload(context.Background())
				require.NoError(t, err)
				encoded, err := json.Marshal(payload)
				require.NoError(t, err)

				// The fields are exactly the Worker's, and so are the features'
				assert.Equal(t, sorted(workerList(t, source, "FIELDS")), keysOf(t, encoded))
				var object struct {
					Features json.RawMessage `json:"features"`
				}
				require.NoError(t, json.Unmarshal(encoded, &object))
				assert.Equal(t, sorted(workerList(t, source, "FEATURE_KEYS")), keysOf(t, object.Features))

				// Every value is one the Worker accepts
				assert.Regexp(t, uuidV4, payload.InstanceID)
				assert.Regexp(t, versionRule, payload.Version)
				assert.Contains(t, workerList(t, source, "MODES"), payload.Mode)
				assert.Contains(t, workerList(t, source, "DBS"), payload.DB)
				assert.Regexp(t, osRule, payload.OS)
				assert.LessOrEqual(t, len(payload.OS), 32)
				assert.Contains(t, workerList(t, source, "MONITOR_BUCKETS"), payload.Monitors)
				for _, name := range append(append([]string(nil), payload.MonitorTypes...), payload.NotificationChannelTypes...) {
					assert.Regexp(t, typeRule, name)
				}
			})
		}
	}
}

func TestBuildPayload_AgreesWithTheWorkersTypeRulesAndBuckets(t *testing.T) {
	source := workerValidatorSource(t)

	// The type rule and the cap are the Worker's
	assert.Equal(t, workerPattern(t, source, "TYPE_NAME").String(), typeName.String())
	assert.Equal(t, workerNumber(t, source, "MAX_TYPES"), maxTypes)

	// The buckets the server can produce are exactly the Worker's
	produced := map[string]bool{}
	for _, count := range []int{0, 1, 10, 11, 50, 51, 200, 201, 1000, 1001, 100000} {
		produced[monitorBucket(count)] = true
	}
	var buckets []string
	for bucket := range produced {
		buckets = append(buckets, bucket)
	}
	assert.Equal(t, sorted(workerList(t, source, "MONITOR_BUCKETS")), sorted(buckets))
}

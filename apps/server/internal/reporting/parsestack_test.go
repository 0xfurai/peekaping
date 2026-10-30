package reporting_test

import (
	"fmt"
	"strings"
	"testing"

	"peekaping/internal/reporting"
	"peekaping/internal/reporting/reportingtest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// frame is the part of a reported frame that comes from the stack text
type frame struct {
	Function string
	Module   string
	Filename string
	Lineno   int
	InApp    bool
}

// reportedFrames reports a panic with the stack text and returns the frames of the report, the
// oldest first
func reportedFrames(t *testing.T, stack string) []frame {
	t.Helper()
	recorder := reportingtest.Install(t)
	reporting.CapturePanic("http", "boom", []byte(stack))
	require.Len(t, recorder.Events(), 1)
	var out []frame
	for _, f := range recorder.Events()[0].Exception[0].Stacktrace.Frames {
		out = append(out, frame{f.Function, f.Module, f.Filename, f.Lineno, f.InApp})
	}
	return out
}

func TestCapturePanic_ReadsTheFramesOfAStackText(t *testing.T) {
	tests := []struct {
		name  string
		stack string
		want  []frame // the oldest first
	}{
		{
			name: "the hook's own frames are cut, the rest is turned around and a file path is cut to its package and name",
			stack: "goroutine 9 [running]:\n" +
				"runtime/debug.Stack()\n\t/go/src/runtime/debug/stack.go:26 +0x5e\n" +
				"peekaping/internal/reporting.CapturePanic({0x1, 0x2})\n\t/src/apps/server/internal/reporting/reporting.go:10 +0x1\n" +
				"peekaping/internal/httpx.Recovery.func1.1()\n\t/src/apps/server/internal/httpx/middleware.go:14 +0x4f\n" +
				"main.main()\n\tC:\\build\\app\\main.go:7 +0x2\n",
			want: []frame{
				{"main", "main", "main/main.go", 7, true},
				{"Recovery.func1.1", "peekaping/internal/httpx", "peekaping/internal/httpx/middleware.go", 14, true},
			},
		},
		{
			name: "a pointer receiver, arguments and a dependency's package",
			stack: "peekaping/internal/monitor.(*Service).Check(0x14000, {0x10, 0x20})\n\t/src/apps/server/internal/monitor/service.go:88 +0x1c\n" +
				"github.com/uptrace/bun.(*DB).Exec(0x1)\n\t/home/u/go/pkg/mod/github.com/uptrace/bun@v1/db.go:3 +0x1\n",
			want: []frame{
				{"(*DB).Exec", "github.com/uptrace/bun", "github.com/uptrace/bun/db.go", 3, false},
				{"(*Service).Check", "peekaping/internal/monitor", "peekaping/internal/monitor/service.go", 88, true},
			},
		},
		{
			name: "a stack with a panic keeps what called the code that panicked",
			stack: "runtime/debug.Stack()\n\t/go/src/runtime/debug/stack.go:26 +0x5e\n" +
				"peekaping/internal/httpx.Recovery.func1.1()\n\t/src/httpx/middleware.go:14 +0x4f\n" +
				"panic({0x1048a0, 0x1048b8})\n\t/go/src/runtime/panic.go:860 +0x13c\n" +
				"runtime.goPanicIndex(0x7, 0x3)\n\t/go/src/runtime/panic.go:115 +0x1\n" +
				"peekaping/internal/x.Work(0x1)\n\t/src/x/work.go:5 +0x1\n" +
				"peekaping/internal/x.Run()\n\t/src/x/run.go:9 +0x1\n",
			want: []frame{
				{"Run", "peekaping/internal/x", "peekaping/internal/x/run.go", 9, true},
				{"Work", "peekaping/internal/x", "peekaping/internal/x/work.go", 5, true},
			},
		},
		{
			name: "a stack with a panic and nothing but the runtime after it keeps the cut of a stack without one",
			stack: "runtime/debug.Stack()\n\t/go/src/runtime/debug/stack.go:26 +0x5e\n" +
				"panic({0x1})\n\t/go/src/runtime/panic.go:860 +0x13c\n" +
				"runtime.goPanicIndex(0x7, 0x3)\n\t/go/src/runtime/panic.go:115 +0x1\n",
			want: []frame{
				{"goPanicIndex", "runtime", "runtime/panic.go", 115, false},
				{"panic", "", "panic.go", 860, false},
			},
		},
		{
			name: "blank lines, a created-by line and a file line with no function are skipped",
			stack: "\n" +
				"\t/orphan/file.go:1 +0x1\n" +
				"peekaping/internal/x.A()\n\t/src/x/a.go:2 +0x1\n\n" +
				"created by peekaping/internal/x.Start in goroutine 1\n\t/src/x/start.go:5 +0x9\n",
			want: []frame{{"A", "peekaping/internal/x", "peekaping/internal/x/a.go", 2, true}},
		},
		{
			name: "indentation by spaces, and the first file line that reads wins",
			stack: "peekaping/internal/x.A()\n" +
				"    not a file line\n" +
				"    /src/x/a.go:2 +0x1\n" +
				"\t/src/x/other.go:99 +0x1\n",
			want: []frame{{"A", "peekaping/internal/x", "peekaping/internal/x/a.go", 2, true}},
		},
		{
			name: "file lines that do not read leave the frame without a file",
			stack: "peekaping/internal/x.A()\n\t/src/x/a.go:0 +0x1\n" +
				"peekaping/internal/x.B()\n\t/src/x/b.go:abc\n" +
				"peekaping/internal/x.C()\n\tnocolon\n" +
				"peekaping/internal/x.D()\n\t:7\n" +
				"peekaping/internal/x.E()\n",
			want: []frame{
				{"E", "peekaping/internal/x", "", 0, true},
				{"D", "peekaping/internal/x", "", 0, true},
				{"C", "peekaping/internal/x", "", 0, true},
				{"B", "peekaping/internal/x", "", 0, true},
				{"A", "peekaping/internal/x", "", 0, true},
			},
		},
		{
			name: "a function line that is not one is no frame, and the last frame is kept",
			stack: "goroutine 1 [running]:\n" +
				"(0x1)\n\t/src/ignored.go:1 +0x1\n" +
				"main.last()\n\t/src/last.go:3 +0x1",
			want: []frame{{"last", "main", "main/last.go", 3, true}},
		},
		{
			name:  "a function with no package",
			stack: "someFunction(0x1)\n\t/src/f.go:3 +0x1\n",
			want:  []frame{{"someFunction", "", "f.go", 3, false}},
		},
		{
			name:  "nothing at all",
			stack: "",
			want:  nil,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, reportedFrames(t, tc.stack))
		})
	}
}

// The innermost hundred are kept: a hundred exactly are all kept, and a hundred and one lose the oldest
func TestCapturePanic_CutsTheStackAtAHundredFramesFromTheInnermost(t *testing.T) {
	stackOf := func(n int) string {
		var stack strings.Builder
		for i := 0; i < n; i++ {
			fmt.Fprintf(&stack, "peekaping/internal/x.F%d(0x1)\n\t/src/x.go:%d +0x1\n", i, i+1)
		}
		return stack.String()
	}

	frames := reportedFrames(t, stackOf(100))
	require.Len(t, frames, 100)
	assert.Equal(t, "F99", frames[0].Function)
	assert.Equal(t, "F0", frames[99].Function)

	frames = reportedFrames(t, stackOf(101))
	require.Len(t, frames, 100)
	assert.Equal(t, "F99", frames[0].Function, "F100, the oldest, is the one that goes")
	assert.Equal(t, "F0", frames[99].Function)
}

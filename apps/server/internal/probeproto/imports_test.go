package probeproto_test

import (
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The probe binary is built from this package, so it must not pull in the server: no import from
// the project, and none from outside the standard library either.
func TestPackage_ImportsNothingFromTheProject(t *testing.T) {
	files, err := filepath.Glob("*.go")
	require.NoError(t, err)

	checked := 0
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		checked++
		parsed, err := parser.ParseFile(token.NewFileSet(), file, nil, parser.ImportsOnly)
		require.NoError(t, err)
		for _, spec := range parsed.Imports {
			path, err := strconv.Unquote(spec.Path.Value)
			require.NoError(t, err)
			assert.False(t, strings.HasPrefix(path, "peekaping/"), "%s imports %s", file, path)
			// A standard library path has no dot in its first element
			first, _, _ := strings.Cut(path, "/")
			assert.NotContains(t, first, ".", "%s imports %s, which is not in the standard library", file, path)
		}
	}
	require.NotZero(t, checked, "the package has files to check")
}

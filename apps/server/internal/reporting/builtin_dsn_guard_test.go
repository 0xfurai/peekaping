package reporting_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"

	"peekaping/internal/reporting"

	"github.com/getsentry/sentry-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultDSN_IsPeekapingsSentryProject(t *testing.T) {
	dsn, err := sentry.NewDsn(reporting.DefaultDSN)

	require.NoError(t, err)
	assert.Equal(t, "o90647.ingest.us.sentry.io", dsn.GetHost())
	assert.Equal(t, "4512301050691584", dsn.GetProjectID())
}

// Error reports are on by default and reporting.DefaultDSN is a real project, so a test that sets
// reporting up without a DSN of its own would send a panic to Peekaping's Sentry. This reads every
// Go test file of the server and fails on a call that could do that:
//   - reporting.Setup with an Options literal that sets none of DSN, Transport and ForcedOff: true
//     (ForcedOff builds no client at all, which is how a test resets reporting)
//   - app.SetupErrorReporting without the option that gives it a transport
//   - any use of reporting.DefaultDSN
func TestNoTestCanReachTheBuiltInDSN(t *testing.T) {
	serverRoot := filepath.Join("..", "..")
	fset := token.NewFileSet()
	var problems []string
	require.NoError(t, filepath.WalkDir(serverRoot, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		isTest := strings.HasSuffix(path, "_test.go") || strings.Contains(filepath.ToSlash(path), "/reportingtest/")
		if !isTest || filepath.Base(path) == "builtin_dsn_guard_test.go" {
			return nil
		}
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		inReportingPackage := file.Name.Name == "reporting"
		ast.Inspect(file, func(node ast.Node) bool {
			switch n := node.(type) {
			case *ast.SelectorExpr:
				if x, ok := n.X.(*ast.Ident); ok && x.Name == "reporting" && n.Sel.Name == "DefaultDSN" {
					problems = append(problems, fset.Position(n.Pos()).String()+": uses reporting.DefaultDSN")
				}
			case *ast.CallExpr:
				name := calledName(n)
				if inReportingPackage && name == "Setup" {
					name = "reporting.Setup"
				}
				switch name {
				case "reporting.Setup":
					if len(n.Args) != 1 || !keepsOffTheBuiltInDSN(n.Args[0]) {
						problems = append(problems, fset.Position(n.Pos()).String()+": reporting.Setup without a DSN, a Transport or ForcedOff: true")
					}
				case "SetupErrorReporting", "app.SetupErrorReporting", "wiring.SetupErrorReporting":
					if len(n.Args) < 3 {
						problems = append(problems, fset.Position(n.Pos()).String()+": SetupErrorReporting without a transport option")
					}
				}
			}
			return true
		})
		return nil
	}))

	assert.Empty(t, problems)
}

// calledName is "pkg.Func" or "Func" for the function a call names
func calledName(call *ast.CallExpr) string {
	switch fun := call.Fun.(type) {
	case *ast.Ident:
		return fun.Name
	case *ast.SelectorExpr:
		if x, ok := fun.X.(*ast.Ident); ok {
			return x.Name + "." + fun.Sel.Name
		}
	}
	return ""
}

// keepsOffTheBuiltInDSN reports whether arg is an Options literal whose client cannot send to the
// built-in DSN: it sets a DSN or a Transport (anything but the empty string or nil), or ForcedOff
// to true. A field named with its zero value, such as ForcedOff: false, leaves the built-in DSN in
// use, and so does a literal this cannot read, such as a variable.
func keepsOffTheBuiltInDSN(arg ast.Expr) bool {
	lit, ok := arg.(*ast.CompositeLit)
	if !ok {
		return false
	}
	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		key, ok := kv.Key.(*ast.Ident)
		if !ok {
			continue
		}
		switch key.Name {
		case "DSN":
			if value, ok := kv.Value.(*ast.BasicLit); !ok || value.Kind != token.STRING || value.Value != `""` {
				return true
			}
		case "Transport":
			if value, ok := kv.Value.(*ast.Ident); !ok || value.Name != "nil" {
				return true
			}
		case "ForcedOff":
			if value, ok := kv.Value.(*ast.Ident); ok && value.Name == "true" {
				return true
			}
		}
	}
	return false
}

// The guard accepts an Options literal only when the client it builds cannot send to the built-in
// DSN: a DSN or a Transport that is really set, or ForcedOff set to true. Naming a field with its
// zero value ("ForcedOff: false", `DSN: ""`, "Transport: nil") leaves the built-in DSN in use.
func TestNoTestCanReachTheBuiltInDSN_JudgesOptionsLiterals(t *testing.T) {
	for literal, safe := range map[string]bool{
		`reporting.Options{DSN: "http://public@127.0.0.1:9/1"}`:  true,
		`reporting.Options{Transport: transport}`:                true,
		`reporting.Options{ForcedOff: true}`:                     true,
		`reporting.Options{Release: "1", ForcedOff: true}`:       true,
		`reporting.Options{ForcedOff: false}`:                    false,
		`reporting.Options{ForcedOff: forcedOff}`:                false,
		`reporting.Options{DSN: ""}`:                             false,
		`reporting.Options{Transport: nil}`:                      false,
		`reporting.Options{Release: "1", Mode: "single"}`:        false,
		`reporting.Options{ForcedOff: false, DSN: "http://x/1"}`: true,
		`reporting.Options{}`:                                    false,
		`options`:                                                false,
	} {
		expr, err := parser.ParseExpr(literal)
		require.NoError(t, err, literal)

		assert.Equal(t, safe, keepsOffTheBuiltInDSN(expr), literal)
	}
}

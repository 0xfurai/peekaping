package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestToPascalCase(t *testing.T) {
	for in, want := range map[string]string{
		"status_page":  "StatusPage",
		"status-page":  "StatusPage",
		"status page":  "StatusPage",
		"API_KEY":      "ApiKey",
		"monitor":      "Monitor",
		"__a__b__":     "AB",
		"":             "",
		"already_Done": "AlreadyDone",
	} {
		assert.Equal(t, want, ToPascalCase(in), in)
	}
}

// generator is the generate_module command, built once for the tests that run it
func generator(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "generate")
	build := exec.Command("go", "build", "-o", bin, ".")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build the generator: %v\n%s", err, output)
	}
	return bin
}

// runGenerator runs the generator in dir and returns what it printed and its exit code
func runGenerator(t *testing.T, bin, dir string, args ...string) (stdout string, code int) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Dir = dir
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return out.String(), exit.ExitCode()
	}
	require.NoError(t, err)
	return out.String(), 0
}

func write(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}

func TestGenerateModule_WritesOneFileForEachTemplateOfTheModule(t *testing.T) {
	bin, dir := generator(t), t.TempDir()
	write(t, filepath.Join(dir, "templates", "module", "model.go.tmpl"), "package {{.ModuleName}}\n\ntype {{pascal .ModuleName}}Model struct{}\n")
	write(t, filepath.Join(dir, "templates", "module", "names.txt.tmpl"), "{{lower .ModuleName}} {{upper .ModuleName}} {{plural .ModuleName}} {{pascal .ModuleName}}")
	write(t, filepath.Join(dir, "templates", "module", "README.md"), "not a template")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "templates", "module", "nested.tmpl"), 0o755))

	stdout, code := runGenerator(t, bin, dir, "Audit_Log")

	assert.Zero(t, code)
	assert.Equal(t, "Generated src/modules/audit_log/audit_log.model.go\n"+
		"Generated src/modules/audit_log/audit_log.names.txt\n"+
		"\nModule 'audit_log' has been generated successfully!\n"+
		"Next steps:\n"+
		"1. Add your module dependencies in the New<ModuleName> function\n"+
		"2. Implement your module-specific logic in the Upsert method\n"+
		"3. Add your module to the dependency injection setup\n", stdout)
	model, err := os.ReadFile(filepath.Join(dir, "src", "modules", "audit_log", "audit_log.model.go"))
	require.NoError(t, err)
	assert.Equal(t, "package audit_log\n\ntype AuditLogModel struct{}\n", string(model))
	names, err := os.ReadFile(filepath.Join(dir, "src", "modules", "audit_log", "audit_log.names.txt"))
	require.NoError(t, err)
	assert.Equal(t, "audit_log AUDIT_LOG audit_logs AuditLog", string(names))
	entries, err := os.ReadDir(filepath.Join(dir, "src", "modules", "audit_log"))
	require.NoError(t, err)
	assert.Len(t, entries, 2, "a file that is not a template and a directory are left out")
}

func TestGenerateModule_GeneratesTheModuleTemplatesOfTheServer(t *testing.T) {
	bin, dir := generator(t), t.TempDir()
	templates, err := filepath.Glob("../../templates/module/*.tmpl")
	require.NoError(t, err)
	require.NotEmpty(t, templates)
	for _, path := range templates {
		content, err := os.ReadFile(path)
		require.NoError(t, err)
		write(t, filepath.Join(dir, "templates", "module", filepath.Base(path)), string(content))
	}

	stdout, code := runGenerator(t, bin, dir, "widget")

	assert.Zero(t, code, stdout)
	for _, name := range []string{"controller", "dig", "dto", "model", "repository", "route", "service"} {
		assert.FileExists(t, filepath.Join(dir, "src", "modules", "widget", "widget."+name+".go"))
	}
	model, err := os.ReadFile(filepath.Join(dir, "src", "modules", "widget", "widget.model.go"))
	require.NoError(t, err)
	assert.Contains(t, string(model), "package widget\n")
}

func TestGenerateModule_NeedsExactlyOneModuleName(t *testing.T) {
	bin, dir := generator(t), t.TempDir()

	for _, args := range [][]string{nil, {"a", "b"}} {
		stdout, code := runGenerator(t, bin, dir, args...)

		assert.Equal(t, 1, code)
		assert.Equal(t, "Usage: go run scripts/generate_module.go <module_name>\n", stdout)
	}
	assert.NoDirExists(t, filepath.Join(dir, "src"))
}

func TestGenerateModule_StopsAtTheFirstThingItCantDo(t *testing.T) {
	t.Run("no templates directory", func(t *testing.T) {
		bin, dir := generator(t), t.TempDir()

		stdout, code := runGenerator(t, bin, dir, "widget")

		assert.Equal(t, 1, code)
		assert.Equal(t, "Error reading template directory: open templates/module: no such file or directory\n", stdout)
		assert.DirExists(t, filepath.Join(dir, "src", "modules", "widget"), "the module directory is made first")
	})

	t.Run("a module directory that can't be made", func(t *testing.T) {
		bin, dir := generator(t), t.TempDir()
		write(t, filepath.Join(dir, "src"), "a file where the directory goes")

		stdout, code := runGenerator(t, bin, dir, "widget")

		assert.Equal(t, 1, code)
		assert.Contains(t, stdout, "Error creating module directory: ")
	})

	t.Run("a template that can't be read", func(t *testing.T) {
		bin, dir := generator(t), t.TempDir()
		write(t, filepath.Join(dir, "templates", "module", "a.go.tmpl"), "package {{.ModuleName}}")
		require.NoError(t, os.Symlink(filepath.Join(dir, "nowhere"), filepath.Join(dir, "templates", "module", "b.go.tmpl")))
		write(t, filepath.Join(dir, "templates", "module", "c.go.tmpl"), "package {{.ModuleName}}")

		stdout, code := runGenerator(t, bin, dir, "widget")

		assert.Equal(t, 1, code)
		assert.Equal(t, "Generated src/modules/widget/widget.a.go\n"+
			"Error reading template b.go.tmpl: open templates/module/b.go.tmpl: no such file or directory\n", stdout)
		assert.NoFileExists(t, filepath.Join(dir, "src", "modules", "widget", "widget.c.go"), "it stops there")
	})

	t.Run("a template that doesn't parse", func(t *testing.T) {
		bin, dir := generator(t), t.TempDir()
		write(t, filepath.Join(dir, "templates", "module", "a.go.tmpl"), "package {{.ModuleName}}")
		write(t, filepath.Join(dir, "templates", "module", "b.go.tmpl"), "{{.ModuleName")

		stdout, code := runGenerator(t, bin, dir, "widget")

		assert.Equal(t, 1, code)
		assert.Contains(t, stdout, "Generated src/modules/widget/widget.a.go\nError parsing template b.go.tmpl: ")
		assert.NoFileExists(t, filepath.Join(dir, "src", "modules", "widget", "widget.b.go"), "the file is made once the template parses")
	})

	t.Run("an output file that can't be made", func(t *testing.T) {
		bin, dir := generator(t), t.TempDir()
		write(t, filepath.Join(dir, "templates", "module", "a.go.tmpl"), "package {{.ModuleName}}")
		require.NoError(t, os.MkdirAll(filepath.Join(dir, "src", "modules", "widget", "widget.a.go"), 0o755))

		stdout, code := runGenerator(t, bin, dir, "widget")

		assert.Equal(t, 1, code)
		assert.Contains(t, stdout, "Error creating output file src/modules/widget/widget.a.go: ")
	})

	t.Run("a template that doesn't run", func(t *testing.T) {
		bin, dir := generator(t), t.TempDir()
		write(t, filepath.Join(dir, "templates", "module", "a.go.tmpl"), "package {{.Missing}}")

		stdout, code := runGenerator(t, bin, dir, "widget")

		assert.Equal(t, 1, code)
		assert.Contains(t, stdout, "Error executing template a.go.tmpl: ")
		assert.FileExists(t, filepath.Join(dir, "src", "modules", "widget", "widget.a.go"), "the file is made before the template runs")
	})
}

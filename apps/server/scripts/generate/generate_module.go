package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"
	"unicode"

	"github.com/jinzhu/inflection"
)

type ModuleData struct {
	ModuleName string
}

func ToPascalCase(s string) string {
	parts := strings.FieldsFunc(s, func(r rune) bool {
		return r == '_' || r == '-' || unicode.IsSpace(r)
	})

	for i, part := range parts {
		if len(part) > 0 {
			parts[i] = strings.ToUpper(part[:1]) + strings.ToLower(part[1:])
		}
	}

	return strings.Join(parts, "")
}

// templateNames are the names of the files of the template directory that are templates
func templateNames(files []os.DirEntry) []string {
	var templates []string
	for _, file := range files {
		if !file.IsDir() && strings.HasSuffix(file.Name(), ".tmpl") {
			templates = append(templates, file.Name())
		}
	}
	return templates
}

// generateFile writes the output of a template, and exits with a message when it can't
func generateFile(tmpl, outputPath string, funcs template.FuncMap, data ModuleData) {
	templatePath := filepath.Join("templates", "module", tmpl)

	// Read template
	tmplContent, err := os.ReadFile(templatePath)
	if err != nil {
		fmt.Printf("Error reading template %s: %v\n", tmpl, err)
		os.Exit(1)
	}

	// Parse and execute template
	t, err := template.New(tmpl).Funcs(funcs).Parse(string(tmplContent))
	if err != nil {
		fmt.Printf("Error parsing template %s: %v\n", tmpl, err)
		os.Exit(1)
	}

	// Create output file
	f, err := os.Create(outputPath)
	if err != nil {
		fmt.Printf("Error creating output file %s: %v\n", outputPath, err)
		os.Exit(1)
	}

	// Execute template
	if err := t.Execute(f, data); err != nil {
		fmt.Printf("Error executing template %s: %v\n", tmpl, err)
		os.Exit(1)
	}

	f.Close()
}

func main() {
	if len(os.Args) != 2 {
		fmt.Println("Usage: go run scripts/generate_module.go <module_name>")
		os.Exit(1)
	}

	moduleName := strings.ToLower(os.Args[1])
	data := ModuleData{
		ModuleName: moduleName,
	}

	// Create module directory
	moduleDir := filepath.Join("src", "modules", moduleName)
	if err := os.MkdirAll(moduleDir, 0755); err != nil {
		fmt.Printf("Error creating module directory: %v\n", err)
		os.Exit(1)
	}

	templateDir := filepath.Join("templates", "module")
	files, err := os.ReadDir(templateDir)
	if err != nil {
		fmt.Printf("Error reading template directory: %v\n", err)
		os.Exit(1)
	}

	funcs := template.FuncMap{
		"lower":  strings.ToLower,
		"pascal": ToPascalCase,
		"upper":  strings.ToUpper,
		"plural": inflection.Plural,
	}

	for _, tmpl := range templateNames(files) {
		outputPath := filepath.Join(moduleDir, moduleName+"."+strings.Replace(tmpl, ".tmpl", "", 1))
		generateFile(tmpl, outputPath, funcs, data)
		fmt.Printf("Generated %s\n", outputPath)
	}

	fmt.Printf("\nModule '%s' has been generated successfully!\n", moduleName)
	fmt.Println("Next steps:")
	fmt.Println("1. Add your module dependencies in the New<ModuleName> function")
	fmt.Println("2. Implement your module-specific logic in the Upsert method")
	fmt.Println("3. Add your module to the dependency injection setup")
}

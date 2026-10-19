// Command openapi exports the Huma specification without database/Redis access.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"peekaping/internal"
)

func main() {
	format := flag.String("format", "json", "Output format: json or yaml")
	flag.Parse()
	spec := internal.OpenAPI()
	var data []byte
	var err error
	switch *format {
	case "json":
		data, err = json.MarshalIndent(spec, "", "  ")
	case "yaml":
		data, err = spec.YAML()
	default:
		err = fmt.Errorf("unknown format %q", *format)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if _, err := os.Stdout.Write(append(data, '\n')); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

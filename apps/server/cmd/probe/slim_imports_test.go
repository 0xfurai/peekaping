package main

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// heavyDrivers are the modules the slim build (-tags slim) leaves out, each with the monitor type
// whose executor needs it. A module counts as linked when the probe imports it or any package
// under it.
var heavyDrivers = []struct{ module, monitorType string }{
	{"github.com/docker/docker", "docker"},
	{"github.com/IBM/sarama", "kafka-producer"},
	{"github.com/denisenkom/go-mssqldb", "sqlserver"},
	{"go.mongodb.org/mongo-driver", "mongodb"},
	{"google.golang.org/grpc", "grpc-keyword"},
}

// linked lists the packages of the module that are in the graph, sorted
func linked(imports map[string][]string, module string) []string {
	var pkgs []string
	for pkg := range imports {
		if pkg == module || strings.HasPrefix(pkg, module+"/") {
			pkgs = append(pkgs, pkg)
		}
	}
	slices.Sort(pkgs)
	return pkgs
}

// The slim probe is smaller because the drivers of the Docker, Kafka, SQL Server, MongoDB and gRPC
// executors are not linked into it, however an import would bring them in. A failure names the
// chain of imports that does. The test lists the slim build's imports itself, so a plain
// go test checks it.
func TestSlimProbeLinksNoHeavyDrivers(t *testing.T) {
	goBin, ok := goTool()
	if !ok {
		t.Skip("the go command is not available, so the probe's imports can't be listed")
	}
	_, full := importGraph(t, goBin, "")
	root, slim := importGraph(t, goBin, "slim")

	var problems []string
	for _, driver := range heavyDrivers {
		// Guards against a driver that was renamed or dropped, which would pass for nothing
		require.NotEmpty(t, linked(full, driver.module), "the full probe no longer links %s, for the %s monitor", driver.module, driver.monitorType)

		// One chain per module is enough to find the import: the shortest of its packages
		var shortest []string
		for _, pkg := range linked(slim, driver.module) {
			if path := chain(slim, root, pkg); shortest == nil || len(path) < len(shortest) {
				shortest = path
			}
		}
		if shortest != nil {
			problems = append(problems, fmt.Sprintf("%s is linked (it is the %s monitor's driver):\n\t%s", driver.module, driver.monitorType, strings.Join(shortest, "\n\t-> ")))
		}
	}
	slices.Sort(problems)
	if len(problems) > 0 {
		t.Errorf("the slim probe must link none of the heavy executors' drivers, but it links:\n\n%s", strings.Join(problems, "\n\n"))
	}
}

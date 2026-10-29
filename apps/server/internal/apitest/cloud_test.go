package apitest

import (
	"os"

	"peekaping/internal/config"
)

// cloudEnv switches the whole package to the settings of Peekaping Cloud when it is set to 1. CI
// runs the package twice, once with the self-host defaults and once with this, so that what a
// guard of the cloud adds can't break an API route unnoticed (PP-153, docs/adr/0005).
const cloudEnv = "PEE_TEST_CLOUD"

// cloudConfig applies the cloud's settings to the config of an app under test, before the test's
// own options, so that a test that needs one of the self-host defaults sets it itself. It does
// nothing yet: the guards of the cloud add to it as they land, each in its own ticket:
//
//   - open signup (PP-155)
//   - monitor types that are disabled (PP-157)
//   - the private address block (PP-156)
//   - rate limits (PP-159)
func cloudConfig(cfg *config.Config) {
}

// cloudRun reports whether this run uses the cloud's settings
func cloudRun() bool { return os.Getenv(cloudEnv) == "1" }

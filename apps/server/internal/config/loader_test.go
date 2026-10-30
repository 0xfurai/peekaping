package config

import (
	"io"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// loaded has a field of each kind the loader reads, and one it ignores
type loaded struct {
	Name        string        `env:"NAME" default:"fallback"`
	Count       int           `env:"COUNT" default:"5"`
	Big         int64         `env:"BIG"`
	Wait        time.Duration `env:"WAIT" default:"1m"`
	On          bool          `env:"ON" default:"true"`
	Off         bool          `env:"OFF" default:"false"`
	Ratio       float64       `env:"RATIO" default:"1.5"`
	NoEnv       string        `default:"x"`
	BadDefault  int           `env:"BAD_DEFAULT" default:"many"`
	BadDuration time.Duration `env:"BAD_DURATION" default:"soon"`
	hidden      string        `env:"HIDDEN"`
}

// stdout returns what fn prints to os.Stdout
func stdout(t *testing.T, fn func()) string {
	t.Helper()
	original := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w
	defer func() { os.Stdout = original }()
	fn()
	require.NoError(t, w.Close())
	out, err := io.ReadAll(r)
	require.NoError(t, err)
	return string(out)
}

func TestApplyDefaults_FillsTheZeroFieldsItCanAndNamesThemByEnvironmentVariable(t *testing.T) {
	cfg := loaded{Count: 7}

	applied := applyDefaults(&cfg)

	assert.Equal(t, loaded{Name: "fallback", Count: 7, Wait: time.Minute, On: true, NoEnv: "x"}, cfg)
	assert.Equal(t, map[string]string{"NAME": "fallback", "WAIT": "1m", "ON": "true", "OFF": "false"}, applied,
		"a default is named by its variable, and a field with no variable is filled without a name")
}

func TestApplyDefaults_KeepsAFieldThatIsSet(t *testing.T) {
	cfg := loaded{Name: "mine", Wait: time.Second, On: true}

	applied := applyDefaults(&cfg)

	assert.Equal(t, "mine", cfg.Name)
	assert.Equal(t, time.Second, cfg.Wait)
	assert.NotContains(t, applied, "NAME")
	assert.NotContains(t, applied, "WAIT")
	assert.NotContains(t, applied, "ON")
}

func TestSetFieldsFromMap_ReadsEachKindFromItsText(t *testing.T) {
	cfg := loaded{Name: "kept", Count: 3}

	err := setFieldsFromMap(&cfg, map[string]string{
		"NAME":   "",
		"BIG":    "9000000000",
		"WAIT":   "90s",
		"ON":     "1",
		"OFF":    "F",
		"RATIO":  "2.5",
		"HIDDEN": "secret",
		"OTHER":  "unread",
	})

	require.NoError(t, err)
	assert.Equal(t, loaded{Name: "kept", Count: 3, Big: 9000000000, Wait: 90 * time.Second, On: true}, cfg,
		"an empty value, a float and an unexported field are left alone")
}

func TestSetFieldsFromMap_ReadsABooleanAsParseBoolDoes(t *testing.T) {
	for value, want := range map[string]bool{"true": true, "1": true, "TRUE": true, "t": true, "false": false, "0": false, "False": false} {
		cfg := loaded{}
		require.NoError(t, setFieldsFromMap(&cfg, map[string]string{"ON": value}))
		assert.Equal(t, want, cfg.On, value)
	}
}

func TestSetFieldsFromMap_RefusesABooleanParseBoolDoesNotReadAndKeepsTheField(t *testing.T) {
	cfg := loaded{On: true}

	err := setFieldsFromMap(&cfg, map[string]string{"ON": "yes"})

	var invalid *InvalidBooleanError
	require.ErrorAs(t, err, &invalid)
	assert.Equal(t, InvalidBooleanError{Name: "ON", Value: "yes"}, *invalid)
	assert.True(t, cfg.On)
}

func TestSetFieldsFromMap_WarnsAboutANumberThatDoesNotParseAndKeepsTheField(t *testing.T) {
	cfg := loaded{Count: 3, Wait: time.Second}

	out := stdout(t, func() {
		require.NoError(t, setFieldsFromMap(&cfg, map[string]string{"COUNT": "many", "WAIT": "soon", "BIG": "12"}))
	})

	assert.Equal(t, "Warning: could not parse COUNT=many as number: strconv.ParseInt: parsing \"many\": invalid syntax\n"+
		"Warning: could not parse WAIT=soon as number: time: invalid duration \"soon\"\n", out,
		"the warnings follow the order of the struct's fields")
	assert.Equal(t, loaded{Count: 3, Wait: time.Second, Big: 12}, cfg)
}

// A rule on a field that fails is named in words, whatever the rule is
func TestValidateConfig_SaysInWordsWhichRuleEachFieldBroke(t *testing.T) {
	RegisterCustomValidators(validate)
	type checked struct {
		Required string        `validate:"required"`
		URL      string        `validate:"url"`
		Number   string        `validate:"numeric"`
		Port     string        `validate:"port"`
		Kind     string        `validate:"db_type"`
		Level    string        `validate:"log_level"`
		Wait     time.Duration `validate:"duration_min=2m"`
		Count    int           `validate:"min=3"`
		Big      int64         `validate:"min=3"`
		Name     string        `validate:"min=3"`
		Cap      int           `validate:"max=5"`
		Mode     string        `validate:"oneof=a b"`
		Letters  string        `validate:"alpha"`
	}
	cfg := checked{URL: "x", Number: "x1", Port: "70000", Kind: "oracle", Level: "loud", Wait: time.Minute,
		Count: 2, Big: 2, Name: "ab", Cap: 6, Mode: "c", Letters: "1"}

	err := validateConfig(&cfg)

	assert.EqualError(t, err, "validation errors: Required is required; URL must be a valid URL; "+
		"Number must be a valid number; Port must be a valid port number (1-65535); "+
		"Kind must be one of: postgres, postgresql, mysql, sqlite; "+
		"Level must be one of: debug, info, warn, warning, error, dpanic, panic, fatal; "+
		"Wait must be at least 2m; Count must be at least 3; Big must be at least 3; "+
		"Name must be at least 3 characters; Cap must be at most 5; Mode must be one of: a b; "+
		"Letters failed validation: alpha")
}

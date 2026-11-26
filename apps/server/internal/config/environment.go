package config

// Environment remembers which variables the environment set: the system environment or the .env
// file, as opposed to a default. LoadConfig fills it for a config that embeds it. The install
// settings read it (package installsettings): a setting whose variable is set takes the
// environment's value, and the admin area can't change it (docs/adr/0005 section 5).
type Environment struct {
	names map[string]bool
}

// FromEnvironment is whether the system environment or the .env file set the variable name. A
// variable set to an empty string counts as unset, as it does for the value.
func (e Environment) FromEnvironment(name string) bool { return e.names[name] }

// SetFromEnvironment marks names as set by the environment. LoadConfig calls it; a test that
// builds a Config by hand calls it to stand for a variable that is set.
func (e *Environment) SetFromEnvironment(names ...string) {
	if e.names == nil {
		e.names = make(map[string]bool, len(names))
	}
	for _, name := range names {
		e.names[name] = true
	}
}

// environmentRecorder is a config that embeds Environment
type environmentRecorder interface {
	SetFromEnvironment(names ...string)
}

// recordEnvironment marks, on a config that embeds Environment, every variable of sets that has a
// value
func recordEnvironment(config any, sets ...map[string]string) {
	recorder, ok := config.(environmentRecorder)
	if !ok {
		return
	}
	for _, set := range sets {
		for name, value := range set {
			if value != "" {
				recorder.SetFromEnvironment(name)
			}
		}
	}
}

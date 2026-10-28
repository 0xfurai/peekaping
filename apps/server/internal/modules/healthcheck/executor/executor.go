package executor

import (
	"context"
	"fmt"
	"maps"
	"net"
	"peekaping/internal/modules/shared"
	"peekaping/internal/netguard"
	"slices"
	"strings"
	"time"

	"go.uber.org/zap"
)

type Result struct {
	Status    shared.MonitorStatus
	Message   string
	StartTime time.Time
	EndTime   time.Time
	TLSInfo   *shared.TLSInfo `json:"tls_info,omitempty"`
}

type Monitor = shared.Monitor
type Proxy = shared.Proxy

// duplicate of monitor.Model to avoid circular dependency
type ExecutorMonitorParams struct {
	ID             string
	Type           string
	Name           string
	Interval       int
	Timeout        int
	MaxRetries     int
	RetryInterval  int
	ResendInterval int
	Active         bool
	Status         shared.MonitorStatus
	CreatedAt      time.Time
	UpdatedAt      time.Time
	Config         string
}

// Executor defines the interface that all health check executors must implement
type Executor interface {
	Execute(ctx context.Context, params *Monitor, proxyModel *Proxy) *Result
	Validate(configJSON string) error
	Unmarshal(configJSON string) (any, error)
}

// TargetLister is implemented by the executors whose config names hosts they dial. The API asks
// for them when a monitor is saved, so that an address the install blocks (BLOCK_PRIVATE_TARGETS)
// is refused then and not only reported Down at each check.
type TargetLister interface {
	// Targets returns the hosts the config makes the executor dial: the host of a URL, a host, a
	// resolver. Hosts, not host:port. A config that can't be read has none.
	Targets(configJSON string) ([]string, error)
}

// guarded is embedded by the executors that dial through a netguard.Guard. The registry hands the
// guard to each of them once, when it builds them. The zero value has none, which lets everything
// through, as a guard that is off does.
type guarded struct {
	guard *netguard.Guard
}

func (g *guarded) useGuard(guard *netguard.Guard) { g.guard = guard }

// dialer makes d dial through the executor's guard
func (g *guarded) dialer(d *net.Dialer) *net.Dialer { return g.guard.Dialer(d) }

// on reports whether the guard refuses addresses now. An executor that has to change how it connects
// to dial through the guard asks it at each check, so that a check with the option off connects as
// it always did.
func (g *guarded) on() bool { return g.guard.Enabled() }

// hostOf returns the host of a host:port address, without the brackets of an IPv6 address. An
// address without a port is returned as it is, without its brackets.
func hostOf(address string) string {
	address = strings.TrimSpace(address)
	if host, _, err := net.SplitHostPort(address); err == nil {
		return host
	}
	return strings.TrimSuffix(strings.TrimPrefix(address, "["), "]")
}

// guardUser is what the registry looks for in an executor to hand the guard to
type guardUser interface {
	useGuard(*netguard.Guard)
}

// Option changes how NewExecutorRegistry builds the executors
type Option func(*registryOptions)

type registryOptions struct {
	guard *netguard.Guard
}

// WithGuard gives every executor that dials a guard to dial through: with BLOCK_PRIVATE_TARGETS on,
// a check can't reach the addresses the guard blocks. The guard can be turned on after the
// registry is built, which is how a probe follows the server's setting.
func WithGuard(guard *netguard.Guard) Option {
	return func(o *registryOptions) { o.guard = guard }
}

type ExecutorRegistry struct {
	logger   *zap.SugaredLogger
	registry map[string]Executor
	guard    *netguard.Guard
}

// optional holds the constructors of the executors that a slim build (-tags slim) leaves out. The
// file of each one carries //go:build !slim and registers its executor here from init, so a slim
// build neither compiles it nor links the driver it needs. Only the probe is built slim: the
// server, in every mode, has all of them.
var optional = map[string]func(*zap.SugaredLogger) Executor{}

// registerOptional adds the executor of a monitor type that a slim build leaves out to every
// registry NewExecutorRegistry creates
func registerOptional[E Executor](monitorType string, newExecutor func(*zap.SugaredLogger) E) {
	optional[monitorType] = func(logger *zap.SugaredLogger) Executor { return newExecutor(logger) }
}

func NewExecutorRegistry(
	logger *zap.SugaredLogger,
	opts ...Option,
) *ExecutorRegistry {
	var options registryOptions
	for _, opt := range opts {
		opt(&options)
	}
	registry := make(map[string]Executor)

	registry["http"] = NewHTTPExecutor(logger)
	registry["http-keyword"] = NewHTTPExecutor(logger)
	registry["http-json-query"] = NewHTTPExecutor(logger)
	registry["push"] = NewPushExecutor(logger)
	registry["tcp"] = NewTCPExecutor(logger)
	registry["ping"] = NewPingExecutor(logger)
	registry["dns"] = NewDNSExecutor(logger)
	registry["snmp"] = NewSnmpExecutor(logger)
	registry["mysql"] = NewMySQLExecutor(logger)
	registry["postgres"] = NewPostgresExecutor(logger)
	registry["redis"] = NewRedisExecutor(logger)
	registry["mqtt"] = NewMQTTExecutor(logger)
	registry["rabbitmq"] = NewRabbitMQExecutor(logger)
	registry["smtp"] = NewSMTPExecutor(logger)
	// docker, grpc-keyword, mongodb, sqlserver and kafka-producer, unless the build is slim
	for monitorType, newExecutor := range optional {
		registry[monitorType] = newExecutor(logger)
	}

	for _, e := range registry {
		if user, ok := e.(guardUser); ok {
			user.useGuard(options.guard)
		}
	}

	return &ExecutorRegistry{
		registry: registry,
		logger:   logger,
		guard:    options.guard,
	}
}

// func (f *ExecutorRegistry) RegisterExecutor(name string, executor Executor) {
// 	f.registry[name] = executor
// }

func (f *ExecutorRegistry) GetExecutor(name string) (Executor, bool) {
	e, ok := f.registry[name]
	return e, ok
}

// Types lists the monitor types the registry has an executor for, sorted
func (f *ExecutorRegistry) Types() []string {
	return slices.Sorted(maps.Keys(f.registry))
}

// KnownTypes lists the monitor types this build has an executor for, sorted: the types the server
// accepts, which a DISABLED_MONITOR_TYPES entry is checked against
func KnownTypes() []string {
	return NewExecutorRegistry(zap.NewNop().Sugar()).Types()
}

func (er *ExecutorRegistry) ValidateConfig(monitorType string, configJSON string) error {
	executor, ok := er.GetExecutor(monitorType)
	if !ok {
		err := fmt.Errorf("executor not found for monitor type: %s", monitorType)
		return err
	}

	err := executor.Validate(configJSON)
	if err != nil {
		er.logger.Errorf("failed to validate config: %s", err.Error())
		return err
	}

	return er.checkTargets(executor, configJSON)
}

// Targets lists the hosts a check of the monitor type with this config dials. It lists none for a
// type that dials nothing (push), a type the registry doesn't know and a config that can't be read:
// those are left to their own checks to report. It is what the per-host spacing (PP-159) counts a
// check against.
func (er *ExecutorRegistry) Targets(monitorType, configJSON string) []string {
	if er == nil {
		return nil
	}
	e, ok := er.GetExecutor(monitorType)
	if !ok {
		return nil
	}
	lister, ok := e.(TargetLister)
	if !ok {
		return nil
	}
	hosts, err := lister.Targets(configJSON)
	if err != nil {
		return nil
	}
	return slices.DeleteFunc(hosts, func(host string) bool { return strings.TrimSpace(host) == "" })
}

// checkTargets refuses a config that dials an IP address the guard blocks. It does nothing while
// the guard is off, and for an executor that doesn't list its targets. A host that is a name
// passes: what it resolves to is checked when the check dials.
func (er *ExecutorRegistry) checkTargets(e Executor, configJSON string) error {
	lister, ok := e.(TargetLister)
	if !ok || !er.guard.Enabled() {
		return nil
	}
	hosts, err := lister.Targets(configJSON)
	if err != nil {
		return err
	}
	for _, host := range hosts {
		if err := er.guard.CheckLiteral(host); err != nil {
			return err
		}
	}
	return nil
}

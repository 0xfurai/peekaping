package executor

import (
	"context"
	"fmt"
	"maps"
	"peekaping/internal/modules/shared"
	"slices"
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

type ExecutorRegistry struct {
	logger   *zap.SugaredLogger
	registry map[string]Executor
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
) *ExecutorRegistry {
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

	return &ExecutorRegistry{
		registry: registry,
		logger:   logger,
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

	return nil
}

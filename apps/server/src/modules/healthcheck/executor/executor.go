package executor

import (
	"context"
	"fmt"
	"peekaping/src/modules/heartbeat"
	"peekaping/src/modules/shared"
	"time"

	"go.uber.org/zap"
)

type Result struct {
	Status    heartbeat.MonitorStatus
	Message   string
	StartTime time.Time
	EndTime   time.Time
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
	Status         heartbeat.MonitorStatus
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

func NewExecutorRegistry(logger *zap.SugaredLogger, heartbeatService heartbeat.Service) *ExecutorRegistry {
	registry := make(map[string]Executor)

	registry["http"] = NewHTTPExecutor(logger)
	registry["push"] = NewPushExecutor(logger, heartbeatService)
	registry["tcp"] = NewTCPExecutor(logger)
	registry["ping"] = NewPingExecutor(logger)
	registry["dns"] = NewDNSExecutor(logger)
	registry["docker"] = NewDockerExecutor(logger)
	registry["grpc-keyword"] = NewGRPCExecutor(logger)
	registry["snmp"] = NewSnmpExecutor(logger)
	registry["mongodb"] = NewMongoDBExecutor(logger)
	registry["mysql"] = NewMySQLExecutor(logger)
	registry["postgres"] = NewPostgresExecutor(logger)
	registry["sqlserver"] = NewSQLServerExecutor(logger)
	registry["redis"] = NewRedisExecutor(logger)
	registry["mqtt"] = NewMQTTExecutor(logger)
	registry["rabbitmq"] = NewRabbitMQExecutor(logger)
	registry["kafka-producer"] = NewKafkaProducerExecutor(logger)

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

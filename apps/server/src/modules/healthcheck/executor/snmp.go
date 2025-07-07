package executor

import (
	"context"
	"fmt"
	"peekaping/src/modules/shared"
	"strconv"
	"time"

	"github.com/gosnmp/gosnmp"
	"go.uber.org/zap"
)

type SnmpConfig struct {
	Host             string `json:"host" validate:"required" example:"127.0.0.1"`
	Port             uint16 `json:"port" example:"161"`
	Community        string `json:"community" validate:"required" example:"public"`
	SnmpVersion      string `json:"snmp_version" validate:"required,oneof=v1 v2c v3" example:"v2c"`
	Oid              string `json:"oid" validate:"required" example:"1.3.6.1.4.1.1.9.6.1.101"`
	JsonPath         string `json:"json_path" example:"$"`
	JsonPathOperator string `json:"json_path_operator" validate:"omitempty,oneof=eq ne lt gt le ge" example:"eq"`
	ExpectedValue    string `json:"expected_value" example:""`
}

type SnmpExecutor struct {
	logger *zap.SugaredLogger
}

func NewSnmpExecutor(logger *zap.SugaredLogger) *SnmpExecutor {
	return &SnmpExecutor{
		logger: logger,
	}
}

func (s *SnmpExecutor) Unmarshal(configJSON string) (any, error) {
	return GenericUnmarshal[SnmpConfig](configJSON)
}

func (s *SnmpExecutor) Validate(configJSON string) error {
	cfg, err := s.Unmarshal(configJSON)
	if err != nil {
		return err
	}
	return GenericValidator(cfg.(*SnmpConfig))
}

func (s *SnmpExecutor) Execute(ctx context.Context, m *Monitor, proxyModel *Proxy) *Result {
	cfgAny, err := s.Unmarshal(m.Config)
	if err != nil {
		return DownResult(err, time.Now().UTC(), time.Now().UTC())
	}
	cfg := cfgAny.(*SnmpConfig)

	// Set default port if not provided
	if cfg.Port == 0 {
		cfg.Port = 161
	}

	s.logger.Debugf("execute snmp cfg: %+v", cfg)

	startTime := time.Now().UTC()

	// Create SNMP connection
	snmpClient := &gosnmp.GoSNMP{
		Target:    cfg.Host,
		Port:      cfg.Port,
		Community: cfg.Community,
		Version:   s.parseSnmpVersion(cfg.SnmpVersion),
		Timeout:   time.Duration(m.Timeout) * time.Second,
		Retries:   m.MaxRetries,
	}

	err = snmpClient.Connect()
	if err != nil {
		endTime := time.Now().UTC()
		s.logger.Infof("SNMP connection failed: %s, %s", m.Name, err.Error())
		return &Result{
			Status:    shared.MonitorStatusDown,
			Message:   fmt.Sprintf("SNMP connection failed: %v", err),
			StartTime: startTime,
			EndTime:   endTime,
		}
	}
	defer snmpClient.Conn.Close()

	// Perform SNMP GET request
	oids := []string{cfg.Oid}
	result, err := snmpClient.Get(oids)
	if err != nil {
		endTime := time.Now().UTC()
		s.logger.Infof("SNMP GET failed: %s, %s", m.Name, err.Error())
		return &Result{
			Status:    shared.MonitorStatusDown,
			Message:   fmt.Sprintf("SNMP GET failed: %v", err),
			StartTime: startTime,
			EndTime:   endTime,
		}
	}

	endTime := time.Now().UTC()

	if len(result.Variables) == 0 {
		s.logger.Infof("SNMP GET returned no variables: %s", m.Name)
		return &Result{
			Status:    shared.MonitorStatusDown,
			Message:   fmt.Sprintf("No varbinds returned from SNMP session (OID: %s)", cfg.Oid),
			StartTime: startTime,
			EndTime:   endTime,
		}
	}

	variable := result.Variables[0]

	// Check for SNMP errors
	if variable.Type == gosnmp.NoSuchObject {
		s.logger.Infof("SNMP OID not found: %s, %s", m.Name, cfg.Oid)
		return &Result{
			Status:    shared.MonitorStatusDown,
			Message:   fmt.Sprintf("The SNMP query returned that no object exists for OID %s", cfg.Oid),
			StartTime: startTime,
			EndTime:   endTime,
		}
	}

	if variable.Type == gosnmp.NoSuchInstance {
		s.logger.Infof("SNMP instance not found: %s, %s", m.Name, cfg.Oid)
		return &Result{
			Status:    shared.MonitorStatusDown,
			Message:   fmt.Sprintf("The SNMP query returned that no instance exists for OID %s", cfg.Oid),
			StartTime: startTime,
			EndTime:   endTime,
		}
	}

	s.logger.Debugf("SNMP: Received variable (Type: %s Value: %v)", variable.Type, variable.Value)

	// Convert the value to string for comparison
	valueStr := s.convertSnmpValueToString(variable.Value, variable.Type)

	// If no json path or expected value is provided, consider it successful
	if cfg.JsonPath == "" || cfg.ExpectedValue == "" {
		s.logger.Infof("SNMP successful: %s, Value: %s", m.Name, valueStr)
		return &Result{
			Status:    shared.MonitorStatusUp,
			Message:   fmt.Sprintf("SNMP query successful, received value: %s", valueStr),
			StartTime: startTime,
			EndTime:   endTime,
		}
	}

	// Evaluate the condition (simplified version of JSON path evaluation)
	success := s.evaluateCondition(valueStr, cfg.JsonPathOperator, cfg.ExpectedValue)

	if success {
		s.logger.Infof("SNMP condition passed: %s, comparing %s %s %s", m.Name, valueStr, cfg.JsonPathOperator, cfg.ExpectedValue)
		return &Result{
			Status:    shared.MonitorStatusUp,
			Message:   fmt.Sprintf("SNMP condition passes (comparing %s %s %s)", valueStr, cfg.JsonPathOperator, cfg.ExpectedValue),
			StartTime: startTime,
			EndTime:   endTime,
		}
	} else {
		s.logger.Infof("SNMP condition failed: %s, comparing %s %s %s", m.Name, valueStr, cfg.JsonPathOperator, cfg.ExpectedValue)
		return &Result{
			Status:    shared.MonitorStatusDown,
			Message:   fmt.Sprintf("SNMP condition does not pass (comparing %s %s %s)", valueStr, cfg.JsonPathOperator, cfg.ExpectedValue),
			StartTime: startTime,
			EndTime:   endTime,
		}
	}
}

func (s *SnmpExecutor) parseSnmpVersion(version string) gosnmp.SnmpVersion {
	switch version {
	case "v1":
		return gosnmp.Version1
	case "v2c":
		return gosnmp.Version2c
	case "v3":
		return gosnmp.Version3
	default:
		return gosnmp.Version2c // default to v2c
	}
}

func (s *SnmpExecutor) convertSnmpValueToString(value interface{}, valueType gosnmp.Asn1BER) string {
	switch valueType {
	case gosnmp.OctetString:
		if bytes, ok := value.([]byte); ok {
			return string(bytes)
		}
		return fmt.Sprintf("%v", value)
	case gosnmp.Integer:
		return fmt.Sprintf("%d", value)
	case gosnmp.Counter32, gosnmp.Gauge32, gosnmp.TimeTicks, gosnmp.Counter64, gosnmp.Uinteger32:
		return fmt.Sprintf("%d", value)
	case gosnmp.IPAddress:
		if bytes, ok := value.([]byte); ok && len(bytes) == 4 {
			return fmt.Sprintf("%d.%d.%d.%d", bytes[0], bytes[1], bytes[2], bytes[3])
		}
		return fmt.Sprintf("%v", value)
	case gosnmp.ObjectIdentifier:
		return fmt.Sprintf("%v", value)
	default:
		return fmt.Sprintf("%v", value)
	}
}

func (s *SnmpExecutor) evaluateCondition(actual, operator, expected string) bool {
	switch operator {
	case "eq":
		return actual == expected
	case "ne":
		return actual != expected
	case "lt":
		return s.compareNumeric(actual, expected, func(a, b float64) bool { return a < b })
	case "gt":
		return s.compareNumeric(actual, expected, func(a, b float64) bool { return a > b })
	case "le":
		return s.compareNumeric(actual, expected, func(a, b float64) bool { return a <= b })
	case "ge":
		return s.compareNumeric(actual, expected, func(a, b float64) bool { return a >= b })
	default:
		// Default to equality check
		return actual == expected
	}
}

func (s *SnmpExecutor) compareNumeric(actual, expected string, compareFn func(float64, float64) bool) bool {
	actualNum, err1 := strconv.ParseFloat(actual, 64)
	expectedNum, err2 := strconv.ParseFloat(expected, 64)

	if err1 != nil || err2 != nil {
		// If either value is not numeric, fall back to string comparison
		return actual == expected
	}

	return compareFn(actualNum, expectedNum)
}

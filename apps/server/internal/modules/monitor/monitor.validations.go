package monitor

import (
	"sync"

	"peekaping/internal/validation"

	"github.com/go-playground/validator/v10"
)

// registerDtoValidation registers the DTO's rule on the shared validator once. The validator's
// Register methods are not safe while another goroutine validates, and the API tests build many
// servers in one process while others serve requests.
var registerDtoValidation sync.Once

// registerDtoRules makes sure the shared validator knows the DTO's struct-level rule
func registerDtoRules() {
	registerDtoValidation.Do(func() {
		validation.Validate.RegisterStructValidation(CreateUpdateDtoStructLevelValidation, CreateUpdateDto{})
	})
}

// ValidateCreateDto checks a monitor the way the create route does before it calls the service:
// the DTO's field rules, and a timeout of at most 0.8 x the interval. The service's own checks
// (minimum interval, type, links) are not part of it.
func ValidateCreateDto(dto *CreateUpdateDto) error {
	registerDtoRules()
	return validation.Validate.Struct(dto)
}

// timeoutTooLong reports a timeout above 0.8 x the check interval, so that a check has to end
// before the next one is due (in whole seconds, so no rounding).
// With a strict limit no timeout would fit the shortest interval, 20 s with a 16 s minimum.
func timeoutTooLong(timeout, interval int) bool {
	return timeout*5 > interval*4
}

func CreateUpdateDtoStructLevelValidation(sl validator.StructLevel) {
	cfg := sl.Current().Interface().(CreateUpdateDto)

	if timeoutTooLong(cfg.Timeout, cfg.Interval) {
		sl.ReportError(cfg.Timeout, "Timeout", "timeout", "timeout", "")
	}
}

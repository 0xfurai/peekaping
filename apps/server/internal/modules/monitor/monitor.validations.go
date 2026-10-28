package monitor

import (
	"github.com/go-playground/validator/v10"
)

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

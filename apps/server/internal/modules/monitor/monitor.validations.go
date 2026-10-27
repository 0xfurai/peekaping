package monitor

import (
	"github.com/go-playground/validator/v10"
)

func CreateUpdateDtoStructLevelValidation(sl validator.StructLevel) {
	cfg := sl.Current().Interface().(CreateUpdateDto)

	// A check has to end before the next one is due: a timeout is at most 0.8 x the interval
	// (in whole seconds, so no rounding).
	// With a strict limit no timeout would fit the shortest interval, 20 s with a 16 s minimum.
	if cfg.Timeout*5 > cfg.Interval*4 {
		sl.ReportError(cfg.Timeout, "Timeout", "timeout", "timeout", "")
	}
}

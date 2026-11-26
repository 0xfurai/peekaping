package maintenance

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"peekaping/internal/modules/maintenance/utils"
)

// flow is a service over mocks that note, in order, the calls the service makes to them
type flow struct {
	service  *ServiceImpl
	repo     *MockRepository
	monitors *MockMonitorMaintenanceService
	cron     *MockCronGenerator
	timeUtil *MockTimeUtils
	checker  *MockTimeWindowChecker
	validate *MockValidator
	calls    []string
}

func newFlow() *flow {
	service, repo, monitors, cron, checker, timeUtil, validate := createTestService()
	return &flow{service: service, repo: repo, monitors: monitors, cron: cron, timeUtil: timeUtil, checker: checker, validate: validate}
}

// note is what a mock runs when it is called, to put the call in the log
func (f *flow) note(call string) func(mock.Arguments) {
	return func(mock.Arguments) { f.calls = append(f.calls, call) }
}

// expectValidation lets the validation pass, noting the cron and duration it was given
func (f *flow) expectValidation(seen *utils.ValidationParams) {
	f.validate.On("ValidateCronAndDuration", mock.Anything).Return(nil).Run(func(args mock.Arguments) {
		f.calls = append(f.calls, "validate")
		if seen != nil {
			*seen = *args.Get(0).(*utils.ValidationParams)
		}
	})
}

func strPtr(s string) *string { return &s }
func numPtr(n int) *int       { return &n }

const windowID = "window-1"

// writes are the two ways a window is written whole: the operation, and the calls it makes
// around the write
var writes = []struct {
	name string
	// check are the calls that look for the window and the monitors, in order, after the schedule
	check []string
	// expect makes the repository answer the write with result, or err
	expect func(f *flow, result *Model, err error)
	run    func(f *flow, entity *Model, monitorIDs []string) (*Model, error)
}{
	{
		name:  "create",
		check: []string{"require monitors"},
		expect: func(f *flow, result *Model, err error) {
			f.repo.On("Create", mock.Anything, testWorkspace, mock.Anything).Return(result, err).Run(f.note("write"))
		},
		run: func(f *flow, entity *Model, monitorIDs []string) (*Model, error) {
			return f.service.Create(context.Background(), testWorkspace, entity, monitorIDs)
		},
	},
	{
		name:  "update full",
		check: []string{"find window", "require monitors"},
		expect: func(f *flow, result *Model, err error) {
			f.repo.On("FindByID", mock.Anything, testWorkspace, windowID).Return(&Model{ID: windowID}, nil).Run(f.note("find window"))
			f.repo.On("UpdateFull", mock.Anything, testWorkspace, windowID, mock.Anything).Return(result, err).Run(f.note("write"))
		},
		run: func(f *flow, entity *Model, monitorIDs []string) (*Model, error) {
			return f.service.UpdateFull(context.Background(), testWorkspace, windowID, entity, monitorIDs)
		},
	},
}

// Create and UpdateFull validate the schedule as it comes, fill in the cron and the duration it
// leaves out, and only then look for the window and the monitors and write.
func TestServiceImpl_Writes_FillTheScheduleBeforeTheWrite(t *testing.T) {
	for _, w := range writes {
		t.Run(w.name+"/a cron and a duration are kept", func(t *testing.T) {
			f := newFlow()
			var validated utils.ValidationParams
			f.expectValidation(&validated)
			f.monitors.On("RequireMonitors", mock.Anything, testWorkspace, []string{"m1"}).Return(nil).Run(f.note("require monitors"))
			f.monitors.On("SetMonitors", mock.Anything, mock.Anything, []string{"m1"}).Return(nil).Run(f.note("set monitors"))
			written := &Model{ID: windowID, Title: "written"}
			w.expect(f, written, nil)
			entity := &Model{Strategy: "cron", Cron: strPtr("0 9 * * *"), Duration: numPtr(30), StartTime: strPtr("09:00"), EndTime: strPtr("17:00")}

			got, err := w.run(f, entity, []string{"m1"})

			require.NoError(t, err)
			assert.Same(t, written, got)
			assert.Equal(t, append(append([]string{"validate"}, w.check...), "write", "set monitors"), f.calls)
			assert.Equal(t, utils.ValidationParams{Cron: strPtr("0 9 * * *"), Duration: numPtr(30)}, validated)
			assert.Equal(t, "0 9 * * *", *entity.Cron)
			assert.Equal(t, 30, *entity.Duration)
		})

		for _, cron := range []*string{nil, strPtr("")} {
			name := "no cron"
			if cron != nil {
				name = "an empty cron"
			}
			t.Run(w.name+"/"+name+" is generated from the schedule", func(t *testing.T) {
				f := newFlow()
				f.expectValidation(nil)
				generated := "30 9 * * 1,3"
				f.cron.On("GenerateCronExpression", "recurring-weekday", mock.Anything).Return(&generated, nil).Run(f.note("generate cron"))
				f.monitors.On("RequireMonitors", mock.Anything, testWorkspace, []string(nil)).Return(nil).Run(f.note("require monitors"))
				w.expect(f, &Model{ID: windowID}, nil)
				entity := &Model{
					Strategy: "recurring-weekday", Cron: cron, Duration: numPtr(60),
					StartTime: strPtr("09:30"), EndTime: strPtr("10:30"), Weekdays: []int{1, 3}, DaysOfMonth: []int{4}, IntervalDay: numPtr(2),
				}

				_, err := w.run(f, entity, nil)

				require.NoError(t, err)
				assert.Equal(t, append(append([]string{"validate", "generate cron"}, w.check...), "write"), f.calls)
				assert.Equal(t, &generated, entity.Cron)
				f.cron.AssertCalled(t, "GenerateCronExpression", "recurring-weekday", &utils.CronParams{
					StartTime: strPtr("09:30"), EndTime: strPtr("10:30"), Weekdays: []int{1, 3}, DaysOfMonth: []int{4}, IntervalDay: numPtr(2),
				})
				f.monitors.AssertNotCalled(t, "SetMonitors", mock.Anything, mock.Anything, mock.Anything)
			})
		}

		t.Run(w.name+"/a strategy with no cron to generate leaves the cron as it is", func(t *testing.T) {
			f := newFlow()
			f.expectValidation(nil)
			f.cron.On("GenerateCronExpression", "single", mock.Anything).Return(nil, nil).Run(f.note("generate cron"))
			f.monitors.On("RequireMonitors", mock.Anything, testWorkspace, mock.Anything).Return(nil).Run(f.note("require monitors"))
			w.expect(f, &Model{ID: windowID}, nil)
			empty := ""
			entity := &Model{Strategy: "single", Cron: &empty, Duration: numPtr(5)}

			_, err := w.run(f, entity, nil)

			require.NoError(t, err)
			assert.Same(t, &empty, entity.Cron)
			assert.Equal(t, "", *entity.Cron)
		})

		t.Run(w.name+"/a cron that can't be generated stops it before anything is looked up", func(t *testing.T) {
			f := newFlow()
			f.expectValidation(nil)
			f.cron.On("GenerateCronExpression", mock.Anything, mock.Anything).Return(nil, errors.New("no cron")).Run(f.note("generate cron"))
			w.expect(f, &Model{ID: windowID}, nil)

			got, err := w.run(f, &Model{Strategy: "cron"}, nil)

			assert.EqualError(t, err, "no cron")
			assert.Nil(t, got)
			assert.Equal(t, []string{"validate", "generate cron"}, f.calls)
		})

		t.Run(w.name+"/a duration is calculated from the start and end times", func(t *testing.T) {
			f := newFlow()
			f.expectValidation(nil)
			f.timeUtil.On("CalculateDurationFromTimes", "09:00", "17:30").Return(510, nil).Run(f.note("calculate duration"))
			f.monitors.On("RequireMonitors", mock.Anything, testWorkspace, mock.Anything).Return(nil).Run(f.note("require monitors"))
			w.expect(f, &Model{ID: windowID}, nil)
			entity := &Model{Strategy: "manual", Cron: strPtr("0 9 * * *"), StartTime: strPtr("09:00"), EndTime: strPtr("17:30")}

			_, err := w.run(f, entity, nil)

			require.NoError(t, err)
			assert.Equal(t, append(append([]string{"validate", "calculate duration"}, w.check...), "write"), f.calls)
			require.NotNil(t, entity.Duration)
			assert.Equal(t, 510, *entity.Duration)
		})

		t.Run(w.name+"/a duration that can't be calculated stops it before anything is looked up", func(t *testing.T) {
			f := newFlow()
			f.expectValidation(nil)
			f.timeUtil.On("CalculateDurationFromTimes", "09:00", "bad").Return(0, errors.New("bad time")).Run(f.note("calculate duration"))
			w.expect(f, &Model{ID: windowID}, nil)
			entity := &Model{Strategy: "manual", Cron: strPtr("0 9 * * *"), StartTime: strPtr("09:00"), EndTime: strPtr("bad")}

			got, err := w.run(f, entity, nil)

			assert.EqualError(t, err, "bad time")
			assert.Nil(t, got)
			assert.Nil(t, entity.Duration)
			assert.Equal(t, []string{"validate", "calculate duration"}, f.calls)
		})

		t.Run(w.name+"/no duration is calculated without both times, or with one given", func(t *testing.T) {
			for name, entity := range map[string]*Model{
				"no times":      {Strategy: "manual", Cron: strPtr("0 9 * * *")},
				"no start time": {Strategy: "manual", Cron: strPtr("0 9 * * *"), EndTime: strPtr("17:00")},
				"no end time":   {Strategy: "manual", Cron: strPtr("0 9 * * *"), StartTime: strPtr("09:00")},
				"a duration":    {Strategy: "manual", Cron: strPtr("0 9 * * *"), StartTime: strPtr("09:00"), EndTime: strPtr("17:00"), Duration: numPtr(1)},
			} {
				f := newFlow()
				f.expectValidation(nil)
				f.monitors.On("RequireMonitors", mock.Anything, testWorkspace, mock.Anything).Return(nil).Run(f.note("require monitors"))
				w.expect(f, &Model{ID: windowID}, nil)
				before := entity.Duration

				_, err := w.run(f, entity, nil)

				require.NoError(t, err, name)
				f.timeUtil.AssertNotCalled(t, "CalculateDurationFromTimes", mock.Anything, mock.Anything)
				assert.Equal(t, before, entity.Duration, name)
			}
		})

		t.Run(w.name+"/a schedule that doesn't validate stops it first", func(t *testing.T) {
			f := newFlow()
			f.validate.On("ValidateCronAndDuration", mock.Anything).Return(errors.New("cron needs a duration")).Run(f.note("validate"))
			w.expect(f, &Model{ID: windowID}, nil)

			got, err := w.run(f, &Model{Strategy: "cron", Cron: strPtr("0 9 * * *")}, []string{"m1"})

			assert.EqualError(t, err, "cron needs a duration")
			assert.Nil(t, got)
			assert.Equal(t, []string{"validate"}, f.calls)
		})

		t.Run(w.name+"/the monitors are set after the write, and only when they are given", func(t *testing.T) {
			for name, monitorIDs := range map[string][]string{"none given": nil, "cleared": {}, "given": {"m1", "m2"}} {
				f := newFlow()
				f.expectValidation(nil)
				f.monitors.On("RequireMonitors", mock.Anything, testWorkspace, monitorIDs).Return(nil).Run(f.note("require monitors"))
				f.monitors.On("SetMonitors", mock.Anything, windowID, monitorIDs).Return(nil).Run(f.note("set monitors"))
				w.expect(f, &Model{ID: windowID}, nil)

				_, err := w.run(f, &Model{Strategy: "manual", Cron: strPtr("x"), Duration: numPtr(1)}, monitorIDs)

				require.NoError(t, err, name)
				if monitorIDs == nil {
					f.monitors.AssertNotCalled(t, "SetMonitors", mock.Anything, mock.Anything, mock.Anything)
				} else {
					assert.Equal(t, "set monitors", f.calls[len(f.calls)-1], name)
				}
			}
		})

		t.Run(w.name+"/a write that fails sets no monitors", func(t *testing.T) {
			f := newFlow()
			f.expectValidation(nil)
			f.monitors.On("RequireMonitors", mock.Anything, testWorkspace, mock.Anything).Return(nil).Run(f.note("require monitors"))
			failure := errors.New("write failed")
			w.expect(f, nil, failure)

			got, err := w.run(f, &Model{Strategy: "manual", Cron: strPtr("x"), Duration: numPtr(1)}, []string{"m1"})

			assert.Equal(t, failure, err)
			assert.Nil(t, got)
			f.monitors.AssertNotCalled(t, "SetMonitors", mock.Anything, mock.Anything, mock.Anything)
		})

		t.Run(w.name+"/monitors that can't be set fail it after the write", func(t *testing.T) {
			f := newFlow()
			f.expectValidation(nil)
			f.monitors.On("RequireMonitors", mock.Anything, testWorkspace, mock.Anything).Return(nil).Run(f.note("require monitors"))
			failure := errors.New("link failed")
			f.monitors.On("SetMonitors", mock.Anything, windowID, mock.Anything).Return(failure).Run(f.note("set monitors"))
			w.expect(f, &Model{ID: windowID}, nil)

			got, err := w.run(f, &Model{Strategy: "manual", Cron: strPtr("x"), Duration: numPtr(1)}, []string{"m1"})

			assert.Equal(t, failure, err)
			assert.Nil(t, got)
			assert.Equal(t, "set monitors", f.calls[len(f.calls)-1])
		})
	}
}

// partial is a service for an UpdatePartial, whose repository holds current and notes its reads
func partial(current *Model) *flow {
	f := newFlow()
	f.repo.On("FindByID", mock.Anything, testWorkspace, windowID).Return(current, nil).Run(f.note("find window"))
	return f
}

func (f *flow) updatePartial(patch *UpdateModel, monitorIDs []string) (*Model, error) {
	return f.service.UpdatePartial(context.Background(), testWorkspace, windowID, patch, monitorIDs)
}

var currentWindow = func() *Model {
	return &Model{
		ID: windowID, Strategy: "recurring-weekday",
		StartTime: strPtr("09:00"), EndTime: strPtr("17:00"),
		Weekdays: []int{1, 2}, DaysOfMonth: []int{3}, IntervalDay: numPtr(5),
	}
}

// UpdatePartial reads the window to merge the schedule only when it must: for a strategy that comes
// with the patch, and for a duration it doesn't have.
func TestServiceImpl_UpdatePartial_ReadsTheWindowOnlyWhenItMust(t *testing.T) {
	t.Run("a duration and no strategy: one read, to see the window is there", func(t *testing.T) {
		f := partial(currentWindow())
		var validated utils.ValidationParams
		f.expectValidation(&validated)
		f.monitors.On("RequireMonitors", mock.Anything, testWorkspace, mock.Anything).Return(nil).Run(f.note("require monitors"))
		patch := &UpdateModel{Duration: numPtr(45), Cron: strPtr("0 1 * * *")}
		updated := &Model{ID: windowID, Title: "updated"}
		f.repo.On("UpdatePartial", mock.Anything, testWorkspace, windowID, patch).Return(updated, nil).Run(f.note("write"))

		got, err := f.updatePartial(patch, nil)

		require.NoError(t, err)
		assert.Same(t, updated, got)
		assert.Equal(t, []string{"validate", "find window", "require monitors", "write"}, f.calls)
		assert.Equal(t, utils.ValidationParams{Cron: strPtr("0 1 * * *"), Duration: numPtr(45)}, validated)
	})

	t.Run("a strategy and no duration: three reads", func(t *testing.T) {
		f := partial(currentWindow())
		f.expectValidation(nil)
		f.cron.On("GenerateCronExpression", "cron", mock.Anything).Return(nil, nil).Run(f.note("generate cron"))
		f.timeUtil.On("CalculateDurationFromTimes", "09:00", "17:00").Return(480, nil).Run(f.note("calculate duration"))
		f.monitors.On("RequireMonitors", mock.Anything, testWorkspace, mock.Anything).Return(nil).Run(f.note("require monitors"))
		patch := &UpdateModel{Strategy: strPtr("cron")}
		f.repo.On("UpdatePartial", mock.Anything, testWorkspace, windowID, patch).Return(&Model{ID: windowID}, nil).Run(f.note("write"))

		_, err := f.updatePartial(patch, nil)

		require.NoError(t, err)
		assert.Equal(t, []string{
			"find window", "generate cron", "find window", "calculate duration", "validate", "find window", "require monitors", "write",
		}, f.calls)
		assert.Equal(t, 480, *patch.Duration)
	})

	t.Run("a strategy and a duration: two reads", func(t *testing.T) {
		f := partial(currentWindow())
		f.expectValidation(nil)
		f.cron.On("GenerateCronExpression", "cron", mock.Anything).Return(nil, nil).Run(f.note("generate cron"))
		f.monitors.On("RequireMonitors", mock.Anything, testWorkspace, mock.Anything).Return(nil).Run(f.note("require monitors"))
		patch := &UpdateModel{Strategy: strPtr("cron"), Duration: numPtr(1)}
		f.repo.On("UpdatePartial", mock.Anything, testWorkspace, windowID, patch).Return(&Model{ID: windowID}, nil).Run(f.note("write"))

		_, err := f.updatePartial(patch, nil)

		require.NoError(t, err)
		assert.Equal(t, []string{"find window", "generate cron", "validate", "find window", "require monitors", "write"}, f.calls)
		f.timeUtil.AssertNotCalled(t, "CalculateDurationFromTimes", mock.Anything, mock.Anything)
	})
}

// A window that isn't found, or can't be read, is the answer at the first read of it.
func TestServiceImpl_UpdatePartial_StopsAtTheFirstReadThatFails(t *testing.T) {
	failure := errors.New("read failed")
	for name, patch := range map[string]*UpdateModel{
		"with a strategy":    {Strategy: strPtr("cron"), Duration: numPtr(1)},
		"without a duration": {},
	} {
		t.Run(name+"/not found", func(t *testing.T) {
			f := partial(nil)

			got, err := f.updatePartial(patch, []string{"m1"})

			assert.Equal(t, ErrNotFound, err)
			assert.Nil(t, got)
			assert.Equal(t, []string{"find window"}, f.calls)
		})
		t.Run(name+"/unreadable", func(t *testing.T) {
			f := newFlow()
			f.repo.On("FindByID", mock.Anything, testWorkspace, windowID).Return(nil, failure).Run(f.note("find window"))

			got, err := f.updatePartial(patch, []string{"m1"})

			assert.Equal(t, failure, err)
			assert.Nil(t, got)
			assert.Equal(t, []string{"find window"}, f.calls)
		})
	}
}

// The schedule a changed strategy is generated from is the patch over the window: its start time,
// weekdays, days of month and interval fall back to the window's, and its end time does not.
func TestServiceImpl_UpdatePartial_GeneratesTheCronFromThePatchOverTheWindow(t *testing.T) {
	for _, tt := range []struct {
		name  string
		patch UpdateModel
		want  utils.CronParams
	}{
		{
			name:  "nothing but the strategy",
			patch: UpdateModel{Strategy: strPtr("recurring-weekday"), Duration: numPtr(1)},
			want:  utils.CronParams{StartTime: strPtr("09:00"), Weekdays: []int{1, 2}, DaysOfMonth: []int{3}, IntervalDay: numPtr(5)},
		},
		{
			name: "everything in the patch",
			patch: UpdateModel{
				Strategy: strPtr("recurring-weekday"), Duration: numPtr(1), StartTime: strPtr("10:00"), EndTime: strPtr("11:00"),
				Weekdays: []int{6}, DaysOfMonth: []int{7, 8}, IntervalDay: numPtr(9),
			},
			want: utils.CronParams{StartTime: strPtr("10:00"), EndTime: strPtr("11:00"), Weekdays: []int{6}, DaysOfMonth: []int{7, 8}, IntervalDay: numPtr(9)},
		},
		{
			name:  "an empty list is a list",
			patch: UpdateModel{Strategy: strPtr("recurring-weekday"), Duration: numPtr(1), Weekdays: []int{}, DaysOfMonth: []int{}},
			want:  utils.CronParams{StartTime: strPtr("09:00"), Weekdays: []int{}, DaysOfMonth: []int{}, IntervalDay: numPtr(5)},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := partial(currentWindow())
			f.expectValidation(nil)
			generated := "0 9 * * 1,2"
			f.cron.On("GenerateCronExpression", "recurring-weekday", mock.Anything).Return(&generated, nil)
			f.monitors.On("RequireMonitors", mock.Anything, testWorkspace, mock.Anything).Return(nil)
			patch := tt.patch
			f.repo.On("UpdatePartial", mock.Anything, testWorkspace, windowID, &patch).Return(&Model{ID: windowID}, nil)

			_, err := f.updatePartial(&patch, nil)

			require.NoError(t, err)
			f.cron.AssertCalled(t, "GenerateCronExpression", "recurring-weekday", &tt.want)
			assert.Equal(t, &generated, patch.Cron)
		})
	}

	t.Run("a cron in the patch is kept", func(t *testing.T) {
		f := partial(currentWindow())
		f.expectValidation(nil)
		f.monitors.On("RequireMonitors", mock.Anything, testWorkspace, mock.Anything).Return(nil)
		patch := &UpdateModel{Strategy: strPtr("cron"), Duration: numPtr(1), Cron: strPtr("5 5 * * *")}
		f.repo.On("UpdatePartial", mock.Anything, testWorkspace, windowID, patch).Return(&Model{ID: windowID}, nil)

		_, err := f.updatePartial(patch, nil)

		require.NoError(t, err)
		f.cron.AssertNotCalled(t, "GenerateCronExpression", mock.Anything, mock.Anything)
		assert.Equal(t, "5 5 * * *", *patch.Cron)
	})

	t.Run("an empty cron in the patch is generated", func(t *testing.T) {
		f := partial(currentWindow())
		f.expectValidation(nil)
		generated := "1 1 * * *"
		f.cron.On("GenerateCronExpression", "cron", mock.Anything).Return(&generated, nil)
		f.monitors.On("RequireMonitors", mock.Anything, testWorkspace, mock.Anything).Return(nil)
		patch := &UpdateModel{Strategy: strPtr("cron"), Duration: numPtr(1), Cron: strPtr("")}
		f.repo.On("UpdatePartial", mock.Anything, testWorkspace, windowID, patch).Return(&Model{ID: windowID}, nil)

		_, err := f.updatePartial(patch, nil)

		require.NoError(t, err)
		assert.Equal(t, &generated, patch.Cron)
	})

	t.Run("a strategy with no cron to generate leaves the cron as it is", func(t *testing.T) {
		f := partial(currentWindow())
		f.expectValidation(nil)
		f.cron.On("GenerateCronExpression", "single", mock.Anything).Return(nil, nil)
		f.monitors.On("RequireMonitors", mock.Anything, testWorkspace, mock.Anything).Return(nil)
		patch := &UpdateModel{Strategy: strPtr("single"), Duration: numPtr(1)}
		f.repo.On("UpdatePartial", mock.Anything, testWorkspace, windowID, patch).Return(&Model{ID: windowID}, nil)

		_, err := f.updatePartial(patch, nil)

		require.NoError(t, err)
		assert.Nil(t, patch.Cron)
	})

	t.Run("a cron that can't be generated stops it", func(t *testing.T) {
		f := partial(currentWindow())
		f.cron.On("GenerateCronExpression", "cron", mock.Anything).Return(nil, errors.New("no cron")).Run(f.note("generate cron"))

		got, err := f.updatePartial(&UpdateModel{Strategy: strPtr("cron"), Duration: numPtr(1)}, nil)

		assert.EqualError(t, err, "no cron")
		assert.Nil(t, got)
		assert.Equal(t, []string{"find window", "generate cron"}, f.calls)
	})
}

// A patch with no duration gets the one between its start and end times, each of which falls back
// to the window's.
func TestServiceImpl_UpdatePartial_CalculatesTheDurationFromThePatchOverTheWindow(t *testing.T) {
	for _, tt := range []struct {
		name         string
		current      *Model
		patch        UpdateModel
		start, end   string
		wantDuration bool
	}{
		{"both from the window", currentWindow(), UpdateModel{}, "09:00", "17:00", true},
		{"start from the patch", currentWindow(), UpdateModel{StartTime: strPtr("08:15")}, "08:15", "17:00", true},
		{"end from the patch", currentWindow(), UpdateModel{EndTime: strPtr("18:45")}, "09:00", "18:45", true},
		{"both from the patch", currentWindow(), UpdateModel{StartTime: strPtr("01:00"), EndTime: strPtr("02:00")}, "01:00", "02:00", true},
		{"no start anywhere", &Model{ID: windowID, EndTime: strPtr("17:00")}, UpdateModel{}, "", "", false},
		{"no end anywhere", &Model{ID: windowID, StartTime: strPtr("09:00")}, UpdateModel{}, "", "", false},
		{"no end with a start in the patch", &Model{ID: windowID}, UpdateModel{StartTime: strPtr("09:00")}, "", "", false},
		{"no times at all", &Model{ID: windowID}, UpdateModel{}, "", "", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := partial(tt.current)
			f.expectValidation(nil)
			f.monitors.On("RequireMonitors", mock.Anything, testWorkspace, mock.Anything).Return(nil)
			if tt.wantDuration {
				f.timeUtil.On("CalculateDurationFromTimes", tt.start, tt.end).Return(77, nil)
			}
			patch := tt.patch
			f.repo.On("UpdatePartial", mock.Anything, testWorkspace, windowID, &patch).Return(&Model{ID: windowID}, nil)

			_, err := f.updatePartial(&patch, nil)

			require.NoError(t, err)
			if tt.wantDuration {
				require.NotNil(t, patch.Duration)
				assert.Equal(t, 77, *patch.Duration)
			} else {
				assert.Nil(t, patch.Duration)
				f.timeUtil.AssertNotCalled(t, "CalculateDurationFromTimes", mock.Anything, mock.Anything)
			}
		})
	}

	t.Run("a duration that can't be calculated stops it", func(t *testing.T) {
		f := partial(currentWindow())
		f.timeUtil.On("CalculateDurationFromTimes", "09:00", "17:00").Return(0, errors.New("bad time")).Run(f.note("calculate duration"))

		got, err := f.updatePartial(&UpdateModel{}, nil)

		assert.EqualError(t, err, "bad time")
		assert.Nil(t, got)
		assert.Equal(t, []string{"find window", "calculate duration"}, f.calls)
	})
}

// UpdatePartial validates the schedule after it is filled in, then looks for the window and the
// monitors, writes, and sets the monitors that were given.
func TestServiceImpl_UpdatePartial_ValidatesThenWrites(t *testing.T) {
	t.Run("what is validated has the generated cron and the calculated duration", func(t *testing.T) {
		f := partial(currentWindow())
		var validated utils.ValidationParams
		f.expectValidation(&validated)
		generated := "0 9 * * 1,2"
		f.cron.On("GenerateCronExpression", "recurring-weekday", mock.Anything).Return(&generated, nil)
		f.timeUtil.On("CalculateDurationFromTimes", "09:00", "17:00").Return(480, nil)
		f.monitors.On("RequireMonitors", mock.Anything, testWorkspace, mock.Anything).Return(nil)
		f.repo.On("UpdatePartial", mock.Anything, testWorkspace, windowID, mock.Anything).Return(&Model{ID: windowID}, nil)

		_, err := f.updatePartial(&UpdateModel{Strategy: strPtr("recurring-weekday")}, nil)

		require.NoError(t, err)
		assert.Equal(t, utils.ValidationParams{Cron: &generated, Duration: numPtr(480)}, validated)
	})

	t.Run("a schedule that doesn't validate writes nothing", func(t *testing.T) {
		f := partial(currentWindow())
		f.validate.On("ValidateCronAndDuration", mock.Anything).Return(errors.New("cron needs a duration")).Run(f.note("validate"))

		got, err := f.updatePartial(&UpdateModel{Duration: numPtr(1), Cron: strPtr("0 1 * * *")}, []string{"m1"})

		assert.EqualError(t, err, "cron needs a duration")
		assert.Nil(t, got)
		assert.Equal(t, []string{"validate"}, f.calls)
	})

	t.Run("a write that fails sets no monitors", func(t *testing.T) {
		f := partial(currentWindow())
		f.expectValidation(nil)
		f.monitors.On("RequireMonitors", mock.Anything, testWorkspace, mock.Anything).Return(nil)
		failure := errors.New("write failed")
		f.repo.On("UpdatePartial", mock.Anything, testWorkspace, windowID, mock.Anything).Return(nil, failure)

		got, err := f.updatePartial(&UpdateModel{Duration: numPtr(1)}, []string{"m1"})

		assert.Equal(t, failure, err)
		assert.Nil(t, got)
		f.monitors.AssertNotCalled(t, "SetMonitors", mock.Anything, mock.Anything, mock.Anything)
	})

	t.Run("monitors that can't be set fail it after the write", func(t *testing.T) {
		f := partial(currentWindow())
		f.expectValidation(nil)
		f.monitors.On("RequireMonitors", mock.Anything, testWorkspace, mock.Anything).Return(nil)
		f.repo.On("UpdatePartial", mock.Anything, testWorkspace, windowID, mock.Anything).Return(&Model{ID: windowID}, nil).Run(f.note("write"))
		failure := errors.New("link failed")
		f.monitors.On("SetMonitors", mock.Anything, windowID, []string{"m1"}).Return(failure).Run(f.note("set monitors"))

		got, err := f.updatePartial(&UpdateModel{Duration: numPtr(1)}, []string{"m1"})

		assert.Equal(t, failure, err)
		assert.Nil(t, got)
		assert.Equal(t, "set monitors", f.calls[len(f.calls)-1])
	})
}

// IsUnderMaintenance reads a window in its own time zone, falling back to the default one, and
// asks the checker for the period first and then for the slots of a recurring strategy.
func TestServiceImpl_IsUnderMaintenance_Flow(t *testing.T) {
	kyiv := time.FixedZone("Kyiv", 2*60*60)

	t.Run("time zones", func(t *testing.T) {
		for _, tt := range []struct {
			name     string
			timezone *string
			load     string
		}{
			{"a zone of its own", strPtr("Europe/Kyiv"), "Europe/Kyiv"},
			{"no zone", nil, "Default/Zone"},
			{"an empty zone", strPtr(""), "Default/Zone"},
		} {
			f := newFlow()
			f.timeUtil.On("GetDefaultTimezone").Return("Default/Zone")
			f.timeUtil.On("LoadTimezone", tt.load).Return(kyiv)
			f.checker.On("IsInDateTimePeriod", mock.Anything, mock.Anything, kyiv).Return(true, nil)

			got, err := f.service.IsUnderMaintenance(context.Background(), &Model{Active: true, Strategy: "single", Timezone: tt.timezone})

			require.NoError(t, err, tt.name)
			assert.True(t, got, tt.name)
			f.timeUtil.AssertCalled(t, "LoadTimezone", tt.load)
			f.timeUtil.AssertNumberOfCalls(t, "GetDefaultTimezone", 1)
		}
	})

	t.Run("the checker gets the window as it is", func(t *testing.T) {
		f := newFlow()
		f.timeUtil.On("GetDefaultTimezone").Return("UTC")
		f.timeUtil.On("LoadTimezone", "UTC").Return(time.UTC)
		var seen *utils.TimeWindowParams
		var at time.Time
		f.checker.On("IsInDateTimePeriod", mock.Anything, mock.Anything, time.UTC).Return(true, nil).Run(func(args mock.Arguments) {
			seen, at = args.Get(0).(*utils.TimeWindowParams), args.Get(1).(time.Time)
		})
		window := &Model{
			Active: true, Strategy: "single",
			StartDateTime: strPtr("2026-01-01T09:00"), EndDateTime: strPtr("2026-01-02T09:00"),
			StartTime: strPtr("09:00"), EndTime: strPtr("17:00"), IntervalDay: numPtr(3), Cron: strPtr("0 9 * * *"),
			Duration: numPtr(60), Weekdays: []int{1}, DaysOfMonth: []int{2}, Timezone: strPtr(""),
		}

		_, err := f.service.IsUnderMaintenance(context.Background(), window)

		require.NoError(t, err)
		assert.Equal(t, &utils.TimeWindowParams{
			StartDateTime: strPtr("2026-01-01T09:00"), EndDateTime: strPtr("2026-01-02T09:00"),
			StartTime: strPtr("09:00"), EndTime: strPtr("17:00"), IntervalDay: numPtr(3), Cron: strPtr("0 9 * * *"),
			Duration: numPtr(60), Weekdays: []int{1}, DaysOfMonth: []int{2}, Timezone: strPtr(""),
		}, seen)
		assert.WithinDuration(t, time.Now(), at, time.Minute)
		assert.Equal(t, time.UTC, at.Location())
	})

	t.Run("now is read in the window's zone", func(t *testing.T) {
		f := newFlow()
		f.timeUtil.On("GetDefaultTimezone").Return("UTC")
		f.timeUtil.On("LoadTimezone", "Europe/Kyiv").Return(kyiv)
		var at time.Time
		f.checker.On("IsInDateTimePeriod", mock.Anything, mock.Anything, kyiv).Return(false, nil).Run(func(args mock.Arguments) {
			at = args.Get(1).(time.Time)
		})

		_, err := f.service.IsUnderMaintenance(context.Background(), &Model{Active: true, Strategy: "single", Timezone: strPtr("Europe/Kyiv")})

		require.NoError(t, err)
		assert.Equal(t, kyiv, at.Location())
	})

	t.Run("an inactive or manual window asks for nothing", func(t *testing.T) {
		for name, tt := range map[string]struct {
			window *Model
			want   bool
		}{
			"inactive manual":    {&Model{Active: false, Strategy: "manual"}, false},
			"inactive recurring": {&Model{Active: false, Strategy: "recurring-interval"}, false},
			"manual":             {&Model{Active: true, Strategy: "manual"}, true},
		} {
			f := newFlow()

			got, err := f.service.IsUnderMaintenance(context.Background(), tt.window)

			require.NoError(t, err, name)
			assert.Equal(t, tt.want, got, name)
			f.timeUtil.AssertNotCalled(t, "LoadTimezone", mock.Anything)
		}
	})

	t.Run("a period that can't be checked is the answer", func(t *testing.T) {
		for _, strategy := range []string{"single", "recurring-interval", "cron"} {
			f := newFlow()
			f.timeUtil.On("GetDefaultTimezone").Return("UTC")
			f.timeUtil.On("LoadTimezone", "UTC").Return(time.UTC)
			failure := errors.New("no period")
			f.checker.On("IsInDateTimePeriod", mock.Anything, mock.Anything, time.UTC).Return(true, failure)

			got, err := f.service.IsUnderMaintenance(context.Background(), &Model{Active: true, Strategy: strategy, Cron: strPtr("0 9 * * *")})

			assert.Equal(t, failure, err, strategy)
			assert.False(t, got, strategy)
			f.checker.AssertNotCalled(t, "IsInRecurringIntervalWindow", mock.Anything, mock.Anything, mock.Anything)
			f.checker.AssertNotCalled(t, "IsInCronMaintenanceWindow", mock.Anything, mock.Anything, mock.Anything)
		}
	})

	t.Run("a slot that can't be checked is the answer", func(t *testing.T) {
		for strategy, slot := range map[string]string{"recurring-interval": "IsInRecurringIntervalWindow", "cron": "IsInCronMaintenanceWindow", "recurring-weekday": "IsInCronMaintenanceWindow"} {
			f := newFlow()
			f.timeUtil.On("GetDefaultTimezone").Return("UTC")
			f.timeUtil.On("LoadTimezone", "UTC").Return(time.UTC)
			f.checker.On("IsInDateTimePeriod", mock.Anything, mock.Anything, time.UTC).Return(true, nil)
			failure := errors.New("no slot")
			f.checker.On(slot, mock.Anything, mock.Anything, time.UTC).Return(true, failure)

			got, err := f.service.IsUnderMaintenance(context.Background(), &Model{Active: true, Strategy: strategy, Cron: strPtr("0 9 * * *")})

			assert.Equal(t, failure, err, strategy)
			assert.False(t, got, strategy)
		}
	})

	t.Run("a recurring window with no cron has no slots to ask about", func(t *testing.T) {
		for _, cron := range []*string{nil, strPtr("")} {
			f := newFlow()
			f.timeUtil.On("GetDefaultTimezone").Return("UTC")
			f.timeUtil.On("LoadTimezone", "UTC").Return(time.UTC)
			f.checker.On("IsInDateTimePeriod", mock.Anything, mock.Anything, time.UTC).Return(true, nil)

			got, err := f.service.IsUnderMaintenance(context.Background(), &Model{Active: true, Strategy: "recurring-weekday", Cron: cron})

			require.NoError(t, err)
			assert.False(t, got)
			f.checker.AssertNotCalled(t, "IsInCronMaintenanceWindow", mock.Anything, mock.Anything, mock.Anything)
			f.checker.AssertNotCalled(t, "IsInRecurringIntervalWindow", mock.Anything, mock.Anything, mock.Anything)
		}
	})

	t.Run("the interval strategy never asks about the cron", func(t *testing.T) {
		f := newFlow()
		f.timeUtil.On("GetDefaultTimezone").Return("UTC")
		f.timeUtil.On("LoadTimezone", "UTC").Return(time.UTC)
		f.checker.On("IsInDateTimePeriod", mock.Anything, mock.Anything, time.UTC).Return(true, nil)
		f.checker.On("IsInRecurringIntervalWindow", mock.Anything, mock.Anything, time.UTC).Return(false, nil)

		got, err := f.service.IsUnderMaintenance(context.Background(), &Model{Active: true, Strategy: "recurring-interval", Cron: strPtr("0 9 * * *")})

		require.NoError(t, err)
		assert.False(t, got)
		f.checker.AssertNotCalled(t, "IsInCronMaintenanceWindow", mock.Anything, mock.Anything, mock.Anything)
	})

	t.Run("a single window never asks about slots", func(t *testing.T) {
		f := newFlow()
		f.timeUtil.On("GetDefaultTimezone").Return("UTC")
		f.timeUtil.On("LoadTimezone", "UTC").Return(time.UTC)
		f.checker.On("IsInDateTimePeriod", mock.Anything, mock.Anything, time.UTC).Return(true, nil)

		got, err := f.service.IsUnderMaintenance(context.Background(), &Model{Active: true, Strategy: "single", Cron: strPtr("0 9 * * *")})

		require.NoError(t, err)
		assert.True(t, got)
		f.checker.AssertNotCalled(t, "IsInCronMaintenanceWindow", mock.Anything, mock.Anything, mock.Anything)
		f.checker.AssertNotCalled(t, "IsInRecurringIntervalWindow", mock.Anything, mock.Anything, mock.Anything)
	})
}

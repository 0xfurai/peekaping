package bruteforce

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"go.uber.org/zap"
)

// MockRepository is a mock implementation of the Repository interface
// Only methods used by ServiceImpl are implemented
type MockRepository struct {
	mock.Mock
}

func (m *MockRepository) FindByKey(ctx context.Context, key string) (*Model, error) {
	args := m.Called(ctx, key)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*Model), args.Error(1)
}

func (m *MockRepository) Create(ctx context.Context, model *Model) (*Model, error) {
	args := m.Called(ctx, model)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*Model), args.Error(1)
}

func (m *MockRepository) Update(ctx context.Context, key string, updateModel *UpdateModel) error {
	args := m.Called(ctx, key, updateModel)
	return args.Error(0)
}

func (m *MockRepository) Delete(ctx context.Context, key string) error {
	args := m.Called(ctx, key)
	return args.Error(0)
}

func (m *MockRepository) IsLocked(ctx context.Context, key string) (bool, time.Time, error) {
	args := m.Called(ctx, key)
	return args.Bool(0), args.Get(1).(time.Time), args.Error(2)
}

func (m *MockRepository) OnFailure(ctx context.Context, key string, now time.Time, window time.Duration, max int, lockout time.Duration) (bool, time.Time, error) {
	args := m.Called(ctx, key, now, window, max, lockout)
	return args.Bool(0), args.Get(1).(time.Time), args.Error(2)
}

func TestServiceImpl_IsLocked(t *testing.T) {
	tests := []struct {
		name           string
		key            string
		mockReturn     []interface{}
		expectedLocked bool
		expectedUntil  time.Time
		expectedError  error
	}{
		{
			name:           "successful locked check",
			key:            "test_key",
			mockReturn:     []interface{}{true, time.Now().Add(5 * time.Minute), nil},
			expectedLocked: true,
			expectedUntil:  time.Now().Add(5 * time.Minute),
			expectedError:  nil,
		},
		{
			name:           "not locked",
			key:            "test_key",
			mockReturn:     []interface{}{false, time.Time{}, nil},
			expectedLocked: false,
			expectedUntil:  time.Time{},
			expectedError:  nil,
		},
		{
			name:           "repository error",
			key:            "test_key",
			mockReturn:     []interface{}{false, time.Time{}, errors.New("database error")},
			expectedLocked: false,
			expectedUntil:  time.Time{},
			expectedError:  errors.New("database error"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockRepo := &MockRepository{}
			logger := zap.NewNop().Sugar()
			service := NewService(mockRepo, logger)
			ctx := context.Background()

			mockRepo.On("IsLocked", ctx, tt.key).Return(tt.mockReturn...)

			locked, until, err := service.IsLocked(ctx, tt.key)

			if tt.expectedError != nil {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
			assert.Equal(t, tt.expectedLocked, locked)
			if tt.expectedUntil.IsZero() {
				assert.True(t, until.IsZero())
			} else {
				assert.False(t, until.IsZero())
			}
			mockRepo.AssertExpectations(t)
		})
	}
}

func TestServiceImpl_OnFailure(t *testing.T) {
	tests := []struct {
		name           string
		key            string
		now            time.Time
		window         time.Duration
		max            int
		lockout        time.Duration
		mockReturn     []interface{}
		expectedLocked bool
		expectedUntil  time.Time
		expectedError  error
	}{
		{
			name:           "successful failure with lock",
			key:            "test_key",
			now:            time.Now(),
			window:         1 * time.Minute,
			max:            5,
			lockout:        10 * time.Minute,
			mockReturn:     []interface{}{true, time.Now().Add(10 * time.Minute), nil},
			expectedLocked: true,
			expectedUntil:  time.Now().Add(10 * time.Minute),
			expectedError:  nil,
		},
		{
			name:           "failure without lock",
			key:            "test_key",
			now:            time.Now(),
			window:         1 * time.Minute,
			max:            5,
			lockout:        10 * time.Minute,
			mockReturn:     []interface{}{false, time.Time{}, nil},
			expectedLocked: false,
			expectedUntil:  time.Time{},
			expectedError:  nil,
		},
		{
			name:           "repository error",
			key:            "test_key",
			now:            time.Now(),
			window:         1 * time.Minute,
			max:            5,
			lockout:        10 * time.Minute,
			mockReturn:     []interface{}{false, time.Time{}, errors.New("database error")},
			expectedLocked: false,
			expectedUntil:  time.Time{},
			expectedError:  errors.New("database error"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockRepo := &MockRepository{}
			logger := zap.NewNop().Sugar()
			service := NewService(mockRepo, logger)
			ctx := context.Background()

			mockRepo.On("OnFailure", ctx, tt.key, tt.now, tt.window, tt.max, tt.lockout).Return(tt.mockReturn...)

			locked, until, err := service.OnFailure(ctx, tt.key, tt.now, tt.window, tt.max, tt.lockout)

			if tt.expectedError != nil {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
			assert.Equal(t, tt.expectedLocked, locked)
			if tt.expectedUntil.IsZero() {
				assert.True(t, until.IsZero())
			} else {
				assert.False(t, until.IsZero())
			}
			mockRepo.AssertExpectations(t)
		})
	}
}

func TestServiceImpl_Reset(t *testing.T) {
	tests := []struct {
		name          string
		key           string
		mockError     error
		expectedError error
	}{
		{
			name:          "successful reset",
			key:           "test_key",
			mockError:     nil,
			expectedError: nil,
		},
		{
			name:          "delete error",
			key:           "test_key",
			mockError:     errors.New("delete error"),
			expectedError: errors.New("delete error"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockRepo := &MockRepository{}
			logger := zap.NewNop().Sugar()
			service := NewService(mockRepo, logger)
			ctx := context.Background()

			mockRepo.On("Delete", ctx, tt.key).Return(tt.mockError)

			err := service.Reset(ctx, tt.key)

			if tt.expectedError != nil {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
			mockRepo.AssertExpectations(t)
		})
	}
}

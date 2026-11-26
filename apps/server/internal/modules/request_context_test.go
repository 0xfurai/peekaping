package modules_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"peekaping/internal/httpx"
	"peekaping/internal/modules/tag"
	"peekaping/internal/modules/workspace"
	"peekaping/internal/modules/workspace/workspacetest"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type contextTagService struct {
	tag.Service
	seen context.Context
}

func (s *contextTagService) FindAll(ctx context.Context, _ workspace.ID, _, _ int, _ string) ([]*tag.Model, error) {
	s.seen = ctx
	return nil, nil
}

func TestControllerPropagatesRequestContext(t *testing.T) {
	type requestKey struct{}
	parent := context.WithValue(context.Background(), requestKey{}, "request-value")
	ctx, cancel := context.WithDeadline(parent, time.Now().Add(time.Minute))
	cancel()
	ctx = workspace.WithContext(ctx, workspacetest.ID, workspace.RoleAdmin) // the request of a workspace's route
	service := &contextTagService{}
	router := httpx.NewRouter()
	httpx.Register(router, "GET", "/tags", huma.Operation{Errors: []int{400, 500}}, tag.NewController(service, zap.NewNop().Sugar()).FindAll,
		func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				next.ServeHTTP(w, httpx.WithValue(r, "authType", "jwt"))
			})
		})
	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/tags", nil).WithContext(ctx))
	require.NotNil(t, service.seen)
	require.ErrorIs(t, service.seen.Err(), context.Canceled)
	require.Equal(t, ctx.Done(), service.seen.Done())
	wantDeadline, _ := ctx.Deadline()
	gotDeadline, ok := service.seen.Deadline()
	require.True(t, ok)
	require.Equal(t, wantDeadline, gotDeadline)
	require.Equal(t, "request-value", service.seen.Value(requestKey{}))
	require.Equal(t, "jwt", httpx.Value(service.seen, "authType"))
}

package workspaceport

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"peekaping/internal/httpx"
	"peekaping/internal/modules/workspace/workspacehttp"
	"peekaping/internal/utils"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	"go.uber.org/zap"
)

type ExportInput = httpx.Input[httpx.NoBody, struct{}]
type ExportHeartbeatsInput = httpx.Input[httpx.NoBody, struct{}]

// Controller answers the export routes. Both answer a file: the body is the document or the lines
// themselves, not the API's {message, data} envelope.
type Controller struct {
	service Service
	logger  *zap.SugaredLogger
}

func NewController(service Service, logger *zap.SugaredLogger) *Controller {
	return &Controller{service: service, logger: logger.Named("[workspaceport-controller]")}
}

// fail turns the error of the service into its answer, and a failure that is not one of the role
// rules into 500
func (c *Controller) fail(err error) error {
	if answer := workspacehttp.Fail(err); answer != nil {
		return answer
	}
	c.logger.Errorw("A workspace export failed", "error", err)
	return httpx.Fail(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
}

// download sets the headers of a file answer. The file holds secrets, so nothing may keep a copy.
func download(ctx huma.Context, contentType, filename string) {
	ctx.SetHeader("Content-Type", contentType)
	ctx.SetHeader("Content-Disposition", `attachment; filename="`+filename+`"`)
	ctx.SetHeader("Cache-Control", "no-store")
	ctx.SetStatus(http.StatusOK)
}

// Export answers the workspace document as a JSON file
func (c *Controller) Export(ctx context.Context, _ *ExportInput) (*httpx.Output[func(huma.Context)], error) {
	doc, err := c.service.Export(ctx)
	if err != nil {
		return nil, c.fail(err)
	}
	// The document is built in memory, so it is encoded before the answer starts: a failure is a 500
	var body bytes.Buffer
	encoder := json.NewEncoder(&body)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(doc); err != nil {
		return nil, c.fail(err)
	}
	filename := "peekaping-workspace-" + doc.ExportedAt.UTC().Format("2006-01-02") + ".json"
	return &httpx.Output[func(huma.Context)]{Body: func(hctx huma.Context) {
		download(hctx, "application/json; charset=utf-8", filename)
		_, _ = hctx.BodyWriter().Write(body.Bytes())
	}}, nil
}

// ExportHeartbeats streams the workspace's heartbeat history as NDJSON
func (c *Controller) ExportHeartbeats(ctx context.Context, input *ExportHeartbeatsInput) (*httpx.Output[func(huma.Context)], error) {
	export, err := c.service.PrepareHeartbeatExport(ctx)
	if err != nil {
		return nil, c.fail(err)
	}
	filename := "peekaping-heartbeats-" + export.At().Format("2006-01-02") + ".ndjson"
	return &httpx.Output[func(huma.Context)]{Body: func(hctx huma.Context) {
		download(hctx, "application/x-ndjson", filename)
		_, w := humago.Unwrap(hctx)
		if err := export.Write(ctx, newNDJSONSink(w)); err != nil {
			// The answer is started and cannot say it failed. Ending the connection without the
			// terminating chunk tells the client that the file is cut short.
			c.logger.Errorw("A heartbeat export failed part way", "error", err)
			panic(http.ErrAbortHandler)
		}
	}}, nil
}

// ndjsonSink writes one JSON value per line, and flushes the response when asked to
type ndjsonSink struct {
	encoder    *json.Encoder
	controller *http.ResponseController
}

func newNDJSONSink(w http.ResponseWriter) *ndjsonSink {
	encoder := json.NewEncoder(w)
	encoder.SetEscapeHTML(false)
	return &ndjsonSink{encoder: encoder, controller: http.NewResponseController(w)}
}

func (s *ndjsonSink) Line(value any) error { return s.encoder.Encode(value) }

func (s *ndjsonSink) Flush() error {
	if err := s.controller.Flush(); err != nil && !errors.Is(err, http.ErrNotSupported) {
		return err
	}
	return nil
}

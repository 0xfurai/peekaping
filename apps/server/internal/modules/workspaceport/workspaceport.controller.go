package workspaceport

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"peekaping/internal/httpx"
	"peekaping/internal/modules/workspace"
	"peekaping/internal/modules/workspace/workspacehttp"
	"peekaping/internal/utils"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	"go.uber.org/zap"
)

type ExportInput = httpx.Input[httpx.NoBody, struct{}]
type ExportHeartbeatsInput = httpx.Input[httpx.NoBody, struct{}]
type ImportInput = httpx.Input[Document, struct{}]

// ImportRejectedResponse is the answer to a file that can't be imported: a 400 with the usual
// message, and every problem of the file in a list
type ImportRejectedResponse struct {
	Message string         `json:"message" example:"The file can't be imported: 2 problems"`
	Data    httpx.Null     `json:"data"`
	Error   ImportRejected `json:"error"`
}

type ImportRejected struct {
	Code     string    `json:"code" example:"INVALID_WORKSPACE_FILE"`
	Problems []Problem `json:"problems"`
}

func rejected(message string, found []Problem) error {
	return httpx.Fail(http.StatusBadRequest, ImportRejectedResponse{Message: message, Error: ImportRejected{Code: "INVALID_WORKSPACE_FILE", Problems: found}})
}

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
	c.logger.Errorw("A workspace export or import failed", "error", err)
	return httpx.Fail(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
}

// failImport turns the error of an import into its answer: the problems of the file are a 400 with
// the list, and the rest is the export's
func (c *Controller) failImport(err error) error {
	var invalid *InvalidFileError
	if errors.As(err, &invalid) {
		return rejected(fmt.Sprintf("The file can't be imported: %d problem(s)", len(invalid.Problems)), invalid.Problems)
	}
	return c.fail(err)
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

// Import adds the records of the uploaded workspace document to the workspace. The role is checked
// before the body is read, so that a caller who may not import is refused whatever the body is.
func (c *Controller) Import(ctx context.Context, input *ImportInput) (*httpx.Output[ImportResult], error) {
	if err := workspace.RequireRole(ctx, workspace.RoleOwner); err != nil {
		return nil, c.fail(err)
	}
	doc, err := input.Decode()
	if err != nil {
		return nil, rejected("The body is not a workspace document", []Problem{{Record: RecordFile, Message: err.Error()}})
	}
	result, err := c.service.Import(ctx, &doc)
	if err != nil {
		return nil, c.failImport(err)
	}
	return httpx.Reply(http.StatusOK, *result)
}

package handler

import (
	"net/http"

	pkghttputil "github.com/Wei-Shaw/sub2api/internal/pkg/httputil"
	"go.uber.org/zap"
)

// requestBodyReadFailure maps a request body read error to the response status
// and message: 408 for timeouts, 499 for client disconnects, 413 for the
// configured size limit and 400 for other malformed reads. Memory-budget
// overload (503) is answered earlier by the ingress middleware. Failures
// other than the configured size limit are logged with bounded, payload-free
// diagnostics (kind, declared length, bytes received and read duration) so
// operators can tell a stalled or slow upload from a broken client without
// logging request content.
func requestBodyReadFailure(reqLog *zap.Logger, req *http.Request, err error) (int, string) {
	failure := pkghttputil.DescribeRequestBodyReadFailure(req, err)
	if failure.Kind != pkghttputil.RequestBodyReadErrorMaxBytes {
		logRequestBodyReadFailure(reqLog, failure)
	}
	return failure.Status, failure.ClientMessage()
}

func logRequestBodyReadFailure(reqLog *zap.Logger, failure pkghttputil.RequestBodyReadFailure) {
	if reqLog != nil {
		reqLog.Warn("read request body failed", failure.LogFields()...)
	}
}

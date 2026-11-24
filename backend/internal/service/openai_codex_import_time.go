package service

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"
)

// ParseCodexCredentialTime preserves the native session import's RFC3339,
// Unix-seconds and Unix-milliseconds grammar. Parsing supplies a local expiry
// bound, not authenticated provider metadata.
func ParseCodexCredentialTime(value any) (time.Time, bool) {
	unix := func(value int64) time.Time {
		if value > 1_000_000_000_000 {
			return time.UnixMilli(value).UTC()
		}
		return time.Unix(value, 0).UTC()
	}
	switch v := value.(type) {
	case string:
		v = strings.TrimSpace(v)
		if parsed, err := time.Parse(time.RFC3339Nano, v); err == nil {
			return parsed.UTC(), true
		}
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			return unix(n), true
		}
	case json.Number:
		if n, err := v.Int64(); err == nil {
			return unix(n), true
		}
		if n, err := v.Float64(); err == nil {
			return unix(int64(n)), true
		}
	case float64:
		return unix(int64(v)), true
	case int:
		return unix(int64(v)), true
	case int64:
		return unix(v), true
	}
	return time.Time{}, false
}

package logger

import (
	"context"
	"io"
	"log/slog"
	"os"
	"regexp"
	"strings"
)

var (
	bearerTokenRegex = regexp.MustCompile(`(?i)Bearer\s+[A-Za-z0-9._~+/-]+=*`)
	secretParamRegex = regexp.MustCompile(`(?i)(client_secret|password|secret|access_token)=([^&\s]+)`)
	jsonTokenRegex   = regexp.MustCompile(`(?i)"(access_token|client_secret|refresh_token|password)":\s*"[^"]+"`)
)

// RedactingWriter wraps an io.Writer and scrubs sensitive credentials before writing.
type RedactingWriter struct {
	underlying io.Writer
}

// NewRedactingWriter creates a new writer that sanitizes tokens and secrets.
func NewRedactingWriter(w io.Writer) *RedactingWriter {
	return &RedactingWriter{underlying: w}
}

func (rw *RedactingWriter) Write(p []byte) (n int, err error) {
	sanitized := SanitizeText(string(p))
	_, err = rw.underlying.Write([]byte(sanitized))
	// Always return original len to satisfy io.Writer contract
	return len(p), err
}

// SanitizeText replaces sensitive patterns with redaction placeholders.
func SanitizeText(input string) string {
	res := bearerTokenRegex.ReplaceAllString(input, "Bearer [REDACTED_ACCESS_TOKEN]")
	res = secretParamRegex.ReplaceAllString(res, "$1=[REDACTED]")
	res = jsonTokenRegex.ReplaceAllString(res, `"$1": "[REDACTED]"`)
	return res
}

// RedactingHandler is a slog.Handler that wraps another handler and redacts values.
type RedactingHandler struct {
	next slog.Handler
}

// NewRedactingHandler creates a slog handler with redactions applied to log attributes and messages.
func NewRedactingHandler(next slog.Handler) *RedactingHandler {
	return &RedactingHandler{next: next}
}

func (h *RedactingHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

func (h *RedactingHandler) Handle(ctx context.Context, r slog.Record) error {
	sanitizedMsg := SanitizeText(r.Message)
	newRecord := slog.NewRecord(r.Time, r.Level, sanitizedMsg, r.PC)

	r.Attrs(func(a slog.Attr) bool {
		newRecord.AddAttrs(sanitizeAttr(a))
		return true
	})

	return h.next.Handle(ctx, newRecord)
}

func (h *RedactingHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	sanitized := make([]slog.Attr, len(attrs))
	for i, a := range attrs {
		sanitized[i] = sanitizeAttr(a)
	}
	return &RedactingHandler{next: h.next.WithAttrs(sanitized)}
}

func (h *RedactingHandler) WithGroup(name string) slog.Handler {
	return &RedactingHandler{next: h.next.WithGroup(name)}
}

func sanitizeAttr(a slog.Attr) slog.Attr {
	key := strings.ToLower(a.Key)
	if strings.Contains(key, "secret") ||
		strings.Contains(key, "token") ||
		strings.Contains(key, "password") ||
		strings.Contains(key, "authorization") {
		return slog.String(a.Key, "[REDACTED]")
	}

	if a.Value.Kind() == slog.KindString {
		return slog.String(a.Key, SanitizeText(a.Value.String()))
	}

	return a
}

// InitLogger configures the default global slog logger with structured JSON and redactions.
func InitLogger(levelStr string, w io.Writer) *slog.Logger {
	var level slog.Level
	switch strings.ToLower(levelStr) {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}

	if w == nil {
		w = os.Stdout
	}

	redactingWriter := NewRedactingWriter(w)
	baseHandler := slog.NewJSONHandler(redactingWriter, &slog.HandlerOptions{
		Level: level,
	})

	safeHandler := NewRedactingHandler(baseHandler)
	logger := slog.New(safeHandler)
	slog.SetDefault(logger)
	return logger
}

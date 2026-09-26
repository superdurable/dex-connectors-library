// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package triggerlog holds the Trigger delivery logging helpers that sdkgo and sdkgo/localconfig share.
package triggerlog

import (
	"context"
	"errors"
	"log/slog"
	"net/url"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/superdurable/dex/sdk-go/dex"
)

// Logger writes Trigger delivery records with a fixed set of leading attributes. A Logger without a
// configured *slog.Logger writes to slog.Default() as of each record, so an application that calls
// slog.SetDefault after it builds its runners still receives every record.
type Logger struct {
	logger *slog.Logger
	attrs  []slog.Attr
}

// New returns a Logger that writes to logger, or to slog.Default() when logger is nil, and starts every
// record with attrs.
func New(logger *slog.Logger, attrs ...slog.Attr) Logger {
	return Logger{logger: logger, attrs: attrs}
}

// Slog returns the destination logger with the leading attributes applied, for APIs that take a
// *slog.Logger. It resolves slog.Default() when it is called.
func (log Logger) Slog() *slog.Logger {
	logger := log.destination()
	if len(log.attrs) == 0 {
		return logger
	}
	return slog.New(logger.Handler().WithAttrs(log.attrs))
}

// Debug writes a DEBUG record.
func (log Logger) Debug(ctx context.Context, message string, attrs ...slog.Attr) {
	log.write(ctx, slog.LevelDebug, message, attrs)
}

// Info writes an INFO record.
func (log Logger) Info(ctx context.Context, message string, attrs ...slog.Attr) {
	log.write(ctx, slog.LevelInfo, message, attrs)
}

// Warn writes a WARN record.
func (log Logger) Warn(ctx context.Context, message string, attrs ...slog.Attr) {
	log.write(ctx, slog.LevelWarn, message, attrs)
}

// Error writes an ERROR record.
func (log Logger) Error(ctx context.Context, message string, attrs ...slog.Attr) {
	log.write(ctx, slog.LevelError, message, attrs)
}

func (log Logger) destination() *slog.Logger {
	if log.logger != nil {
		return log.logger
	}
	return slog.Default()
}

// write builds the record itself, so a handler with AddSource reports the SDK function that called
// Debug, Info, Warn, or Error rather than this helper. The record carries the leading attributes, then
// the context's attributes, then attrs.
func (log Logger) write(ctx context.Context, level slog.Level, message string, attrs []slog.Attr) {
	if ctx == nil {
		ctx = context.Background()
	}
	logger := log.destination()
	if !logger.Enabled(ctx, level) {
		return
	}
	var pcs [1]uintptr
	// Skip runtime.Callers, write, and the Debug, Info, Warn, or Error method.
	runtime.Callers(3, pcs[:])
	record := slog.NewRecord(time.Now(), level, message, pcs[0])
	record.AddAttrs(log.attrs...)
	record.AddAttrs(contextAttrs(ctx, log.attrs, attrs)...)
	record.AddAttrs(attrs...)
	_ = logger.Handler().Handle(ctx, record)
}

// Err returns the "error" attribute. It records only err.Error(), never the error's fields, so a
// handler that inspects values cannot reach an event payload through a wrapped error. The query string
// and user information of every URL in a wrapped *url.Error are removed, because a query can carry an
// API key or a signed request.
func Err(err error) slog.Attr {
	return slog.String("error", redactURLQuery(err))
}

// ErrAttrs returns a "flow_id" attribute when err is a Dex error about one Flow, followed by the
// "error" attribute.
func ErrAttrs(err error) []slog.Attr {
	var serviceError *dex.ServiceError
	if errors.As(err, &serviceError) && serviceError.FlowID != "" {
		return []slog.Attr{slog.String("flow_id", serviceError.FlowID), Err(err)}
	}
	return []slog.Attr{Err(err)}
}

func redactURLQuery(err error) string {
	message := err.Error()
	for _, urlError := range wrappedURLErrors(err) {
		if urlError.URL == "" {
			continue
		}
		redacted := "[URL]"
		if parsed, parseErr := url.Parse(urlError.URL); parseErr == nil {
			if parsed.RawQuery == "" && !parsed.ForceQuery && parsed.User == nil {
				continue
			}
			parsed.RawQuery, parsed.ForceQuery, parsed.User = "", false, nil
			redacted = parsed.String()
		}
		// url.Error quotes its URL, so replace the quoted form before the raw one.
		message = strings.ReplaceAll(message, strconv.Quote(urlError.URL), strconv.Quote(redacted))
		message = strings.ReplaceAll(message, urlError.URL, redacted)
	}
	return message
}

// wrappedURLErrors walks the whole error tree, including errors.Join and multi-%w branches, and returns
// every *url.Error in it.
func wrappedURLErrors(err error) []*url.Error {
	var found []*url.Error
	pending := []error{err}
	for len(pending) > 0 {
		current := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		if current == nil {
			continue
		}
		if urlError, ok := current.(*url.Error); ok {
			found = append(found, urlError)
		}
		switch wrapper := current.(type) {
		case interface{ Unwrap() []error }:
			pending = append(pending, wrapper.Unwrap()...)
		case interface{ Unwrap() error }:
			pending = append(pending, wrapper.Unwrap())
		}
	}
	return found
}

type contextAttrsKey struct{}

// ContextWithAttrs returns a context whose Trigger delivery records also carry attrs, after any that ctx
// already carries.
func ContextWithAttrs(ctx context.Context, attrs ...slog.Attr) context.Context {
	if len(attrs) == 0 {
		return ctx
	}
	existing, _ := ctx.Value(contextAttrsKey{}).([]slog.Attr)
	combined := make([]slog.Attr, 0, len(existing)+len(attrs))
	return context.WithValue(ctx, contextAttrsKey{}, append(append(combined, existing...), attrs...))
}

// contextAttrs returns the context's attributes whose keys the record does not already carry.
func contextAttrs(ctx context.Context, leading []slog.Attr, attrs []slog.Attr) []slog.Attr {
	carried, _ := ctx.Value(contextAttrsKey{}).([]slog.Attr)
	if len(carried) == 0 {
		return nil
	}
	taken := make(map[string]bool, len(leading)+len(attrs))
	for _, attr := range leading {
		taken[attr.Key] = true
	}
	for _, attr := range attrs {
		taken[attr.Key] = true
	}
	kept := make([]slog.Attr, 0, len(carried))
	for _, attr := range carried {
		if !taken[attr.Key] {
			taken[attr.Key] = true
			kept = append(kept, attr)
		}
	}
	return kept
}

type skipRecorderKey struct{}

// WithSkipRecorder returns a context for one delivery attempt and a function that reports whether a
// target inside that attempt consumed the event as undeliverable and logged the skip itself.
func WithSkipRecorder(ctx context.Context) (context.Context, func() bool) {
	skipped := &atomic.Bool{}
	return context.WithValue(ctx, skipRecorderKey{}, skipped), skipped.Load
}

// RecordSkip reports to the enclosing delivery attempt, if any, that the caller consumed the event as
// undeliverable and logged the skip. The attempt then does not also log the event as delivered.
func RecordSkip(ctx context.Context) {
	if skipped, ok := ctx.Value(skipRecorderKey{}).(*atomic.Bool); ok {
		skipped.Store(true)
	}
}

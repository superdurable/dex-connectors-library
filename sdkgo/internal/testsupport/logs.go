// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package testsupport

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
)

// LogRecord is one captured slog record with its attributes flattened to strings.
type LogRecord struct {
	Level   slog.Level
	Message string
	Attrs   map[string]string
}

// String renders the record like a text handler, with attributes sorted by key.
func (record LogRecord) String() string {
	keys := make([]string, 0, len(record.Attrs))
	for key := range record.Attrs {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var builder strings.Builder
	fmt.Fprintf(&builder, "level=%s msg=%q", record.Level, record.Message)
	for _, key := range keys {
		fmt.Fprintf(&builder, " %s=%q", key, record.Attrs[key])
	}
	return builder.String()
}

// LogRecorder is a slog.Handler that keeps every record, at every level, for assertions. It is safe
// for concurrent use.
type LogRecorder struct {
	store *logStore
	attrs []slog.Attr
}

type logStore struct {
	mutex   sync.Mutex
	records []LogRecord
}

// NewLogRecorder returns an empty recorder.
func NewLogRecorder() *LogRecorder {
	return &LogRecorder{store: &logStore{}}
}

// Logger returns a logger that writes to the recorder.
func (recorder *LogRecorder) Logger() *slog.Logger {
	return slog.New(recorder)
}

// Enabled records every level, including DEBUG.
func (*LogRecorder) Enabled(context.Context, slog.Level) bool { return true }

// Handle captures one record.
func (recorder *LogRecorder) Handle(_ context.Context, record slog.Record) error {
	captured := LogRecord{Level: record.Level, Message: record.Message, Attrs: map[string]string{}}
	for _, attr := range recorder.attrs {
		captured.Attrs[attr.Key] = attr.Value.Resolve().String()
	}
	record.Attrs(func(attr slog.Attr) bool {
		captured.Attrs[attr.Key] = attr.Value.Resolve().String()
		return true
	})
	recorder.store.mutex.Lock()
	defer recorder.store.mutex.Unlock()
	recorder.store.records = append(recorder.store.records, captured)
	return nil
}

// WithAttrs returns a handler that shares the recorder's records and adds attrs to each.
func (recorder *LogRecorder) WithAttrs(attrs []slog.Attr) slog.Handler {
	combined := append(append([]slog.Attr(nil), recorder.attrs...), attrs...)
	return &LogRecorder{store: recorder.store, attrs: combined}
}

// WithGroup is not used by the SDK; it returns the recorder unchanged.
func (recorder *LogRecorder) WithGroup(string) slog.Handler { return recorder }

// Records returns a copy of every captured record in order.
func (recorder *LogRecorder) Records() []LogRecord {
	recorder.store.mutex.Lock()
	defer recorder.store.mutex.Unlock()
	return append([]LogRecord(nil), recorder.store.records...)
}

// Find returns the captured records with message, in order.
func (recorder *LogRecorder) Find(message string) []LogRecord {
	var found []LogRecord
	for _, record := range recorder.Records() {
		if record.Message == message {
			found = append(found, record)
		}
	}
	return found
}

// Text renders every captured record, one per line, for substring checks such as secret sentinels.
func (recorder *LogRecorder) Text() string {
	var builder strings.Builder
	for _, record := range recorder.Records() {
		builder.WriteString(record.String())
		builder.WriteByte('\n')
	}
	return builder.String()
}

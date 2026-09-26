//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package integrationtest_test

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"testing"
)

// sentinelText stands in for provider message text. No log record may contain it.
const sentinelText = "SENTINEL-MESSAGE-TEXT"

type capturedLogRecord struct {
	level   slog.Level
	message string
	attrs   map[string]string
}

func (record capturedLogRecord) String() string {
	keys := make([]string, 0, len(record.attrs))
	for key := range record.attrs {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var builder strings.Builder
	fmt.Fprintf(&builder, "level=%s msg=%q", record.level, record.message)
	for _, key := range keys {
		fmt.Fprintf(&builder, " %s=%s", key, record.attrs[key])
	}
	return builder.String()
}

// logRecorder is a race-safe slog.Handler that keeps every record, as an application's handler would
// receive it. It writes the records to the test log when the test fails or runs with -v.
type logRecorder struct {
	store *logRecorderStore
	attrs []slog.Attr
}

type logRecorderStore struct {
	mutex   sync.Mutex
	records []capturedLogRecord
}

func newLogRecorder(t *testing.T) *logRecorder {
	t.Helper()
	recorder := &logRecorder{store: &logRecorderStore{}}
	t.Cleanup(func() {
		if t.Failed() || testing.Verbose() {
			t.Logf("captured Trigger logs:\n%s", recorder.text())
		}
	})
	return recorder
}

func (recorder *logRecorder) logger() *slog.Logger { return slog.New(recorder) }

func (*logRecorder) Enabled(context.Context, slog.Level) bool { return true }

func (recorder *logRecorder) Handle(_ context.Context, record slog.Record) error {
	captured := capturedLogRecord{level: record.Level, message: record.Message, attrs: map[string]string{}}
	for _, attr := range recorder.attrs {
		captured.attrs[attr.Key] = attr.Value.Resolve().String()
	}
	record.Attrs(func(attr slog.Attr) bool {
		captured.attrs[attr.Key] = attr.Value.Resolve().String()
		return true
	})
	recorder.store.mutex.Lock()
	defer recorder.store.mutex.Unlock()
	recorder.store.records = append(recorder.store.records, captured)
	return nil
}

func (recorder *logRecorder) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &logRecorder{store: recorder.store, attrs: append(append([]slog.Attr(nil), recorder.attrs...), attrs...)}
}

func (recorder *logRecorder) WithGroup(string) slog.Handler { return recorder }

func (recorder *logRecorder) records() []capturedLogRecord {
	recorder.store.mutex.Lock()
	defer recorder.store.mutex.Unlock()
	return append([]capturedLogRecord(nil), recorder.store.records...)
}

// find returns the records with message whose attributes include every key-value pair in match.
func (recorder *logRecorder) find(message string, match map[string]string) []capturedLogRecord {
	var found []capturedLogRecord
	for _, record := range recorder.records() {
		if record.message != message {
			continue
		}
		matches := true
		for key, value := range match {
			if record.attrs[key] != value {
				matches = false
				break
			}
		}
		if matches {
			found = append(found, record)
		}
	}
	return found
}

func (recorder *logRecorder) text() string {
	var builder strings.Builder
	for _, record := range recorder.records() {
		builder.WriteString(record.String())
		builder.WriteByte('\n')
	}
	return builder.String()
}

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package providerhttp

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
)

var (
	// ErrStreamTooLarge reports that a server-sent event stream exceeded its total byte limit.
	ErrStreamTooLarge = errors.New("server-sent event stream exceeds its size limit")
	// ErrStreamEventTooLarge reports that one server-sent event or line exceeded its byte limit.
	ErrStreamEventTooLarge = errors.New("server-sent event exceeds its size limit")
)

// ServerSentEvent is one dispatched text/event-stream event.
type ServerSentEvent struct {
	// Type is the value of the event's "event" field, or empty when the event
	// named no type, which the specification treats as "message".
	Type string
	// Data joins the values of the event's "data" lines with "\n".
	Data string
}

// ServerSentEventReader reads text/event-stream events within byte limits.
//
// Every byte counts toward the total limit, including ":" comment lines and the
// blank keep-alive lines some providers send while a request waits. The
// per-event limit bounds the field lines of one event and the length of any
// single line, so a peer cannot grow memory without a line break. Comments,
// "id", "retry", and unknown fields are skipped. Lines end with LF or CRLF.
//
// A reader is not safe for concurrent use.
type ServerSentEventReader struct {
	lines         *bufio.Reader
	maxTotalBytes int64
	maxEventBytes int
	totalBytes    int64
}

// NewServerSentEventReader returns a reader over body that fails once the
// stream exceeds maxTotalBytes or one event or line exceeds maxEventBytes.
// The caller keeps ownership of body and closes it.
func NewServerSentEventReader(body io.Reader, maxTotalBytes int64, maxEventBytes int) *ServerSentEventReader {
	return &ServerSentEventReader{
		lines: bufio.NewReaderSize(body, 4096), maxTotalBytes: maxTotalBytes, maxEventBytes: maxEventBytes,
	}
}

// ReadEvent returns the next event that has at least one "data" line.
//
// It returns io.EOF when the stream ends; a partial event without its closing
// blank line is discarded, as the specification requires. It returns
// ErrStreamTooLarge or ErrStreamEventTooLarge when a limit is exceeded, and
// otherwise wraps the error returned by the body, such as a context
// cancellation. After an error the reader must not be used again.
func (reader *ServerSentEventReader) ReadEvent() (ServerSentEvent, error) {
	var (
		eventType  string
		data       bytes.Buffer
		hasData    bool
		eventBytes int
	)
	for {
		line, err := reader.readLine()
		if err != nil {
			return ServerSentEvent{}, err
		}
		if len(line) == 0 {
			if hasData {
				return ServerSentEvent{Type: eventType, Data: data.String()}, nil
			}
			eventType, eventBytes = "", 0
			continue
		}
		if line[0] == ':' {
			continue
		}
		eventBytes += len(line)
		if eventBytes > reader.maxEventBytes {
			return ServerSentEvent{}, ErrStreamEventTooLarge
		}
		field, value := line, []byte(nil)
		if colon := bytes.IndexByte(line, ':'); colon >= 0 {
			field, value = line[:colon], bytes.TrimPrefix(line[colon+1:], []byte(" "))
		}
		switch string(field) {
		case "data":
			if hasData {
				data.WriteByte('\n')
			}
			data.Write(value)
			hasData = true
		case "event":
			eventType = string(value)
		}
	}
}

// readLine returns one line without its terminator, counting every byte read.
func (reader *ServerSentEventReader) readLine() ([]byte, error) {
	var line []byte
	for {
		fragment, err := reader.lines.ReadSlice('\n')
		reader.totalBytes += int64(len(fragment))
		if reader.totalBytes > reader.maxTotalBytes {
			return nil, ErrStreamTooLarge
		}
		if len(line)+len(fragment)-2 > reader.maxEventBytes {
			return nil, ErrStreamEventTooLarge
		}
		line = append(line, fragment...)
		switch {
		case err == nil:
			line = bytes.TrimSuffix(bytes.TrimSuffix(line, []byte("\n")), []byte("\r"))
			return line, nil
		case errors.Is(err, bufio.ErrBufferFull):
			continue
		case errors.Is(err, io.EOF):
			return nil, io.EOF
		default:
			return nil, fmt.Errorf("read server-sent event stream: %w", err)
		}
	}
}

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package singledispatch

import (
	"errors"
	"io"
	"net/http"
	"strings"
)

// PrepareHTTPWrite disables net/http's transparent replay of a fenced request.
// Call it after building the request and before http.Client.Do. It clears
// GetBody and gives bodyless writes a non-replayable empty body. The caller
// must use the request only once; redirects and transport retries are not a
// substitute for the fence's retry decision.
func PrepareHTTPWrite(request *http.Request) error {
	if request == nil {
		return errors.New("fenced HTTP request is nil")
	}
	request.GetBody = nil
	if request.Body == nil || request.Body == http.NoBody {
		request.Body = io.NopCloser(strings.NewReader(""))
	}
	return nil
}

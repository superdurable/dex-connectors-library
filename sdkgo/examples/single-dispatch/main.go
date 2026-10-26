// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatchtest"
)

var errRetry = errors.New("retry the Step")
var errUncertain = errors.New("write outcome needs operator reconciliation")

type noteConnector struct {
	client   *http.Client
	endpoint string
}

func (noteConnector) resolveCredential() (string, error) { return "example-token", nil }
func (connector noteConnector) send(ctx context.Context, credential string) (int, string, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, connector.endpoint, strings.NewReader("example note"))
	if err != nil {
		return 0, "", err
	}
	request.Header.Set("Authorization", "Bearer "+credential)
	request.Header.Set("Idempotency-Key", "example-call")
	if err := singledispatch.PrepareHTTPWrite(request); err != nil {
		return 0, "", err
	}
	response, err := connector.client.Do(request)
	if err != nil {
		return 0, "", err
	}
	if err := response.Body.Close(); err != nil {
		return 0, "", err
	}
	return response.StatusCode, response.Header.Get("X-Note-ID"), nil
}
func (noteConnector) readBack(_ context.Context, providerID string) (bool, error) {
	return providerID == "note-1", nil
}

type noteSendAttempt struct {
	connector  noteConnector
	credential string
	status     int
	providerID string
}

func (attempt *noteSendAttempt) send(ctx context.Context) error {
	var err error
	attempt.status, attempt.providerID, err = attempt.connector.send(ctx, attempt.credential)
	return err
}

func (connector noteConnector) createNote(call sdkgo.Call) (string, error) {
	fence, err := singledispatch.NewFence(call, 30*time.Second)
	if err != nil {
		return "", err
	}
	switch decision := fence.Decide(); decision.Kind {
	case singledispatch.FailClosed:
		return "", errUncertain
	case singledispatch.ReadBackRequired:
		if decision.Marker.CallID != call.ID || decision.Marker.ProviderID == "" {
			return "", errUncertain
		}
		found, err := connector.readBack(call.Context, decision.Marker.ProviderID)
		if err != nil {
			return "", errRetry
		}
		if !found {
			return "", errUncertain
		}
		return decision.Marker.ProviderID, nil
	}
	credential, err := connector.resolveCredential()
	if err != nil {
		if checkpointErr := fence.RecordNotSent(); checkpointErr != nil {
			return "", errors.Join(errRetry, checkpointErr)
		}
		return "", errRetry
	}
	attempt := &noteSendAttempt{connector: connector, credential: credential}
	if err := fence.Dispatch(attempt.send); err != nil {
		if errors.Is(err, singledispatch.ErrCheckpoint) || errors.Is(err, singledispatch.ErrInsufficientTime) {
			return "", errRetry
		}
		return "", errUncertain
	}
	if attempt.status == 503 || attempt.status == 429 {
		if err := fence.RecordCanResend(); err != nil {
			return "", errors.Join(errRetry, err)
		}
		return "", errRetry
	}
	if attempt.status != 201 || attempt.providerID == "" {
		return "", errUncertain
	}
	// The accepted result is known even if its extra read-back checkpoint is lost.
	_ = fence.RecordProviderID(attempt.providerID)
	return attempt.providerID, nil
}

func main() {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost {
			response.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if _, err := io.Copy(io.Discard, request.Body); err != nil {
			response.WriteHeader(http.StatusBadRequest)
			return
		}
		response.Header().Set("X-Note-ID", "note-1")
		response.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()
	ctx := singledispatchtest.NewContext("example-flow", "create-note")
	connector := noteConnector{client: server.Client(), endpoint: server.URL}
	providerID, err := connector.createNote(sdkgo.Call{Context: ctx, ID: "example-call"})
	if err != nil {
		panic(err)
	}
	fmt.Println(providerID)
}

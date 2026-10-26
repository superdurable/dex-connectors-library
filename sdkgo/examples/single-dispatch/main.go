// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatchtest"
)

var errRetry = errors.New("retry the Step")
var errUncertain = errors.New("write outcome needs operator reconciliation")

type noteConnector struct{}

func (noteConnector) resolveCredential() (string, error) { return "example-token", nil }
func (noteConnector) send(_ context.Context, _ string) (int, string, error) {
	return 201, "note-1", nil
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
		if decision.Marker.ProviderID == "" {
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
			return "", checkpointErr
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
			return "", errRetry
		}
		return "", errRetry
	}
	if attempt.status != 201 || attempt.providerID == "" {
		return "", errUncertain
	}
	// The accepted result is known even if its extra read-back checkpoint is lost.
	if err := fence.RecordProviderID(attempt.providerID); err != nil {
		return attempt.providerID, nil
	}
	return attempt.providerID, nil
}

func main() {
	ctx := singledispatchtest.NewContext("example-flow", "create-note")
	providerID, err := (noteConnector{}).createNote(sdkgo.Call{Context: ctx, ID: "example-call"})
	if err != nil {
		panic(err)
	}
	fmt.Println(providerID)
}

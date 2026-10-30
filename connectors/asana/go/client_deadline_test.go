// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package asana

import (
	"context"
	"testing"
	"time"
)

func TestCanStartRequestNeedsOneTenthOfTheAttemptBudget(t *testing.T) {
	now := time.Now()
	client := &Client{now: func() time.Time { return now }}
	requestContext, cancel := context.WithDeadline(context.Background(), now.Add(100*time.Millisecond))
	defer cancel()
	session := &operationSession{context: requestContext, minimumRequestTime: 150 * time.Millisecond}
	if client.canStartRequest(session) {
		t.Fatal("request started with less than the required send window")
	}
	session.minimumRequestTime = 50 * time.Millisecond
	if !client.canStartRequest(session) {
		t.Fatal("request was rejected despite enough time remaining")
	}
}

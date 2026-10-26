// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package slack

import (
	"context"
	"crypto/tls"
	"net/http/httptrace"
	"sync"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const minimumPostRequestTime = 250 * time.Millisecond

type postDispatchCheckpoint struct {
	CallID        sdkgo.CallID `json:"slackPostCallId"`
	Attempt       int32        `json:"slackPostAttempt"`
	IsSafeToRetry bool         `json:"slackPostSafeToRetry"`
}

type postDispatchObservation struct {
	mutex                 sync.Mutex
	hasConnectionFailure  bool
	hasObtainedConnection bool
}

type postDispatchClaim uint8

const (
	postDispatchGranted postDispatchClaim = iota + 1
	postDispatchMayHaveSent
	postDispatchNotRecorded
)

func newPostRequestContext(stepContext context.Context, attemptStart time.Time, methodTimeout time.Duration) (context.Context, context.CancelFunc, bool) {
	available := methodTimeout
	if contextDeadline, hasDeadline := stepContext.Deadline(); hasDeadline {
		available = min(available, contextDeadline.Sub(attemptStart))
	}
	if available <= 0 {
		return stepContext, func() {}, false
	}
	requestDeadline := attemptStart.Add(available * 5 / 6)
	minimumTime := max(minimumPostRequestTime, available/10)
	if time.Until(requestDeadline) < minimumTime {
		return stepContext, func() {}, false
	}
	requestContext, cancel := context.WithDeadline(stepContext, requestDeadline)
	return requestContext, cancel, true
}

// claimPostDispatch fails closed when an earlier attempt cannot prove that a resend is safe.
func claimPostDispatch(call sdkgo.Call) postDispatchClaim {
	var checkpoint postDispatchCheckpoint
	isFound, err := call.Context.GetLastHeartbeatValue(&checkpoint)
	if err != nil {
		return postDispatchMayHaveSent
	}
	attempt := call.Context.Attempt()
	if attempt == 1 {
		if isFound {
			return postDispatchMayHaveSent
		}
	} else if !isFound || checkpoint.CallID != call.ID || checkpoint.Attempt != attempt-1 || !checkpoint.IsSafeToRetry {
		return postDispatchMayHaveSent
	}
	if err := call.Context.RecordHeartbeat(postDispatchCheckpoint{CallID: call.ID, Attempt: attempt}); err != nil {
		return postDispatchNotRecorded
	}
	return postDispatchGranted
}

func markPostSafeToRetry(call sdkgo.Call) {
	// A lost marker leaves the next attempt unable to prove a resend is safe.
	_ = call.Context.RecordHeartbeat(postDispatchCheckpoint{CallID: call.ID, Attempt: call.Context.Attempt(), IsSafeToRetry: true})
}

func (observation *postDispatchObservation) clientTrace() *httptrace.ClientTrace {
	return &httptrace.ClientTrace{
		DNSDone: func(info httptrace.DNSDoneInfo) {
			if info.Err != nil {
				observation.recordConnectionFailure()
			}
		},
		ConnectDone: func(_ string, _ string, err error) {
			if err != nil {
				observation.recordConnectionFailure()
			}
		},
		TLSHandshakeDone: func(_ tls.ConnectionState, err error) {
			if err != nil {
				observation.recordConnectionFailure()
			}
		},
		GotConn: func(httptrace.GotConnInfo) {
			observation.mutex.Lock()
			observation.hasObtainedConnection = true
			observation.mutex.Unlock()
		},
	}
}

func (observation *postDispatchObservation) recordConnectionFailure() {
	observation.mutex.Lock()
	observation.hasConnectionFailure = true
	observation.mutex.Unlock()
}

func (observation *postDispatchObservation) isProvablyUndispatched() bool {
	observation.mutex.Lock()
	defer observation.mutex.Unlock()
	return observation.hasConnectionFailure && !observation.hasObtainedConnection
}

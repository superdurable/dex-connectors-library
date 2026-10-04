// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package front

import (
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"sync"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// rateLimitResetHeader is the Unix second when Front's per-minute window resets.
const rateLimitResetHeader = "X-Ratelimit-Reset"

var errFrontRequestNotBuilt = errors.New("Front request could not be built")

// frontRequest is one Core API request below the client's base URL; path is already escaped.
type frontRequest struct {
	method  string
	path    string
	query   url.Values
	payload any
}

type frontResponse struct {
	statusCode int
	header     http.Header
	body       []byte
}

// exchangeOutcome is the provider-neutral meaning of one Front answer that each operation maps to a branch.
type exchangeOutcome uint8

const (
	exchangeSucceeded exchangeOutcome = iota + 1
	// exchangeMerged is a 301 to the conversation that absorbed the requested one.
	exchangeMerged
	exchangeNotFound
	// exchangeNotSent means a traced connection failure proves Front never received the request.
	exchangeNotSent
	// exchangeRateLimited is a 429, which Front answers before applying the request.
	exchangeRateLimited
	// exchangeUnconfirmed is any other transport failure, a 408, or a 5xx: Front may have applied it.
	exchangeUnconfirmed
	exchangeRejected
	exchangeInvalid
	exchangeDefect
)

// frontExchange is one classified request; failure is set for every outcome except exchangeSucceeded.
type frontExchange struct {
	outcome    exchangeOutcome
	response   frontResponse
	failure    sdkgo.Failure
	retryAfter time.Duration
}

// dispatchObservation records whether a request could have reached Front.
type dispatchObservation struct {
	mutex                 sync.Mutex
	hasObtainedConnection bool
	hasFailedToConnect    bool
}

// isRetryable reports an outcome that a read or an idempotent write may simply repeat.
func (result frontExchange) isRetryable() bool {
	return result.outcome == exchangeNotSent || result.outcome == exchangeRateLimited || result.outcome == exchangeUnconfirmed
}

func (observation *dispatchObservation) clientTrace() *httptrace.ClientTrace {
	return &httptrace.ClientTrace{
		DNSDone:          observation.recordDNSDone,
		ConnectDone:      observation.recordConnectDone,
		TLSHandshakeDone: observation.recordTLSHandshakeDone,
		GotConn:          observation.recordObtainedConnection,
	}
}

// isProvablyUndispatched requires a traced DNS, connect, or TLS failure and no obtained connection.
func (observation *dispatchObservation) isProvablyUndispatched() bool {
	observation.mutex.Lock()
	defer observation.mutex.Unlock()
	return observation.hasFailedToConnect && !observation.hasObtainedConnection
}

func (observation *dispatchObservation) recordDNSDone(info httptrace.DNSDoneInfo) {
	if info.Err != nil {
		observation.recordConnectionFailure()
	}
}

func (observation *dispatchObservation) recordConnectDone(_ string, _ string, err error) {
	if err != nil {
		observation.recordConnectionFailure()
	}
}

func (observation *dispatchObservation) recordTLSHandshakeDone(_ tls.ConnectionState, err error) {
	if err != nil {
		observation.recordConnectionFailure()
	}
}

func (observation *dispatchObservation) recordObtainedConnection(httptrace.GotConnInfo) {
	observation.mutex.Lock()
	defer observation.mutex.Unlock()
	observation.hasObtainedConnection = true
}

func (observation *dispatchObservation) recordConnectionFailure() {
	observation.mutex.Lock()
	defer observation.mutex.Unlock()
	observation.hasFailedToConnect = true
}

// isConnectionNeverEstablished reports a dial failure, after which Front cannot have received the request.
func isConnectionNeverEstablished(err error) bool {
	var operationError *net.OpError
	return errors.As(err, &operationError) && operationError.Op == "dial"
}

func rejectionFailureKind(status int) sdkgo.FailureKind {
	switch status {
	case http.StatusBadRequest, http.StatusUnprocessableEntity, http.StatusRequestEntityTooLarge:
		return sdkgo.FailureValidation
	case http.StatusConflict:
		return sdkgo.FailureConflict
	default:
		return sdkgo.FailureProviderRejection
	}
}

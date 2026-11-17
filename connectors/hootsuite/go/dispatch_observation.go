// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package hootsuite

import (
	"crypto/tls"
	"net/http/httptrace"
	"sync"
)

// dispatchObservation proves a send was not dispatched only from a connection failure
// with no obtained connection; missing trace events never count as proof.
type dispatchObservation struct {
	mutex                 sync.Mutex
	hasConnectionFailure  bool
	hasObtainedConnection bool
}

func (observation *dispatchObservation) clientTrace() *httptrace.ClientTrace {
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

func (observation *dispatchObservation) recordConnectionFailure() {
	observation.mutex.Lock()
	observation.hasConnectionFailure = true
	observation.mutex.Unlock()
}

func (observation *dispatchObservation) isProvablyUndispatched() bool {
	observation.mutex.Lock()
	defer observation.mutex.Unlock()
	return observation.hasConnectionFailure && !observation.hasObtainedConnection
}

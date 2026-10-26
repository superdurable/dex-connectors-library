// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package slack

import (
	"crypto/tls"
	"net/http/httptrace"
	"sync"
)

type postTransportObservation struct {
	mutex                 sync.Mutex
	hasConnectionFailure  bool
	hasObtainedConnection bool
}

func (observation *postTransportObservation) clientTrace() *httptrace.ClientTrace {
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

func (observation *postTransportObservation) recordConnectionFailure() {
	observation.mutex.Lock()
	observation.hasConnectionFailure = true
	observation.mutex.Unlock()
}

func (observation *postTransportObservation) isProvablyUndispatched() bool {
	observation.mutex.Lock()
	defer observation.mutex.Unlock()
	return observation.hasConnectionFailure && !observation.hasObtainedConnection
}

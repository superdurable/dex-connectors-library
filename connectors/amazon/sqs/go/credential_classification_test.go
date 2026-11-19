// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package sqs_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/amazon/sqs/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
)

// erroringCredentials fails every Resolve with one fixed error.
type erroringCredentials struct{ err error }

func (credentials erroringCredentials) Resolve(sdkgo.Call) (sqs.Credentials, error) {
	return sqs.Credentials{}, credentials.err
}

func TestCredentialFailuresSelectDefectOnlyWhenTerminal(t *testing.T) {
	for _, testCase := range []struct {
		name       string
		err        error
		isTerminal bool
	}{
		{"reauthorization required", sdkgo.NewReauthorizationRequiredError(nil), true},
		{"project reauthorization required", fmt.Errorf("refresh: %w", projectconfig.ErrReauthorizationRequired), true},
		{"connection object not found", projectconfig.ErrObjectNotFound, true},
		{"connection not selected", projectconfig.ErrConnectionNotSelected, true},
		{"credential broker rejected", projectconfig.ErrCredentialBrokerRejected, true},
		{"credential broker unavailable", projectconfig.ErrCredentialBrokerUnavailable, false},
		{"static connection not configured", errors.New("connection is not configured"), false},
		{"deadline exceeded", context.DeadlineExceeded, false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			queues := newFakeQueues(t)
			client, err := sqs.New(sqs.Config{Endpoint: queues.URL, Region: testRegion, DefaultQueueURL: queues.QueueURL(ordersQueue)},
				erroringCredentials{err: testCase.err})
			require.NoError(t, err)
			deleted, deleteErr := runMutation(t, newDexContext("credentials"), client.DeleteMessage(), sqs.DeleteMessageInput{ReceiptHandle: "AQEB-handle"})
			if testCase.isTerminal {
				require.NoError(t, deleteErr)
				require.Equal(t, sqs.DeleteMessageBranchDefect, deleted.Branch)
			} else {
				requireRetry(t, deleteErr, sdkgo.FailureAvailability)
			}
		})
	}
}

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package s3_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/amazon/s3/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
)

// erroringCredentials fails every Resolve with one fixed error.
type erroringCredentials struct{ err error }

func (credentials erroringCredentials) Resolve(sdkgo.Call) (s3.Credentials, error) {
	return s3.Credentials{}, credentials.err
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
			store := newFakeStore(t)
			client, err := s3.New(s3.Config{Endpoint: store.URL, Region: testRegion, DefaultBucket: testBucket}, erroringCredentials{err: testCase.err})
			require.NoError(t, err)
			stored, putErr := runMutation(t, newDexContext("credentials"), client.PutObject(), reportInput(false))
			listed, listErr := runQuery(t, "credentials-list", client.ListObjects(), s3.ListObjectsInput{})
			if testCase.isTerminal {
				require.NoError(t, errors.Join(putErr, listErr))
				require.Equal(t, s3.PutObjectBranchDefect, stored.Branch)
				require.Equal(t, s3.ListObjectsBranchDefect, listed.Branch)
			} else {
				requireRetry(t, putErr, sdkgo.FailureAvailability)
				requireRetry(t, listErr, sdkgo.FailureAvailability)
			}
			require.Zero(t, store.ObjectCount())
		})
	}
}

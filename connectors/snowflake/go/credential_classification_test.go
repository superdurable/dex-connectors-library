// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package snowflake_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/snowflake/go"
	"github.com/superdurable/dex-connectors-library/connectors/snowflake/go/internal/fakesnowflake"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
)

// erroringCredentials fails every Resolve with one fixed error.
type erroringCredentials struct{ err error }

func (credentials erroringCredentials) Resolve(sdkgo.Call) (snowflake.Credentials, error) {
	return snowflake.Credentials{}, credentials.err
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
			fake := startFake(t, scriptAlways(fakesnowflake.StatementScript{}))
			client, err := snowflake.New(snowflake.Config{AccountIdentifier: "myorg-analytics"}, erroringCredentials{err: testCase.err},
				snowflake.WithLocalProviderURL(fake.URL), snowflake.WithClock(func() time.Time { return fixedNow }))
			require.NoError(t, err)
			submitted, submitErr := submit(newStep(), client, snowflake.SubmitStatementInput{Statement: "SELECT 1"})
			canceled, cancelErr := cancel(client, "00000000-0000-4000-8000-000000000003")
			if testCase.isTerminal {
				require.NoError(t, submitErr)
				require.NoError(t, cancelErr)
				require.Equal(t, snowflake.SubmitStatementBranchDefect, submitted.Branch)
				require.Equal(t, snowflake.CancelStatementBranchDefect, canceled.Branch)
			} else {
				for _, attemptErr := range []error{submitErr, cancelErr} {
					var retry *sdkgo.RetryError
					require.ErrorAs(t, attemptErr, &retry)
					require.Equal(t, sdkgo.FailureAvailability, retry.Failure.Kind)
				}
			}
			require.Empty(t, fake.Submissions())
		})
	}
}

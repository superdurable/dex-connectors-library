// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package mysql_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/mysql/go"
	"github.com/superdurable/dex-connectors-library/connectors/mysql/go/internal/scriptedmysql"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// erroringCredentials fails every Resolve with one fixed error.
type erroringCredentials struct{ err error }

func (credentials erroringCredentials) Resolve(sdkgo.Call) (mysql.Credentials, error) {
	return mysql.Credentials{}, credentials.err
}

var credentialClassificationCases = []struct {
	name       string
	err        error
	isTerminal bool
}{
	{"reauthorization required", sdkgo.NewReauthorizationRequiredError(nil), true},
	{"project reauthorization required", fmt.Errorf("refresh: %w", projectconfig.ErrReauthorizationRequired), true},
	{"connection object not found", fmt.Errorf("read: %w", projectconfig.ErrObjectNotFound), true},
	{"connection not selected", projectconfig.ErrConnectionNotSelected, true},
	{"credential broker rejected", projectconfig.ErrCredentialBrokerRejected, true},
	{"credential broker unavailable", projectconfig.ErrCredentialBrokerUnavailable, false},
	{"static connection not configured", errors.New("connection is not configured"), false},
	{"deadline exceeded", context.DeadlineExceeded, false},
	{"unlisted error", errors.New("socket reset"), false},
}

func TestExecuteStatementClassifiesCredentialFailures(t *testing.T) {
	for _, testCase := range credentialClassificationCases {
		t.Run(testCase.name, func(t *testing.T) {
			database := newBalanceDatabase(scriptedmysql.Execution{AffectedRows: 1}, scriptedmysql.CommitAndReply)
			server, _ := startScriptedDatabase(t, database, scriptedmysql.Options{}, nil)
			client := newClientWithCredentials(t, server, erroringCredentials{err: testCase.err})
			result, err := execute(t, client, newStep(), addToBalanceInput)
			if testCase.isTerminal {
				require.NoError(t, err)
				require.Equal(t, mysql.ExecuteStatementBranchDefect, result.Branch)
			} else {
				var retry *sdkgo.RetryError
				require.ErrorAs(t, err, &retry)
				require.Equal(t, sdkgo.FailureAvailability, retry.Failure.Kind)
			}
			require.Empty(t, server.EventsOfKind(scriptedmysql.EventHandshake))
		})
	}
}

func TestExecuteStatementNonIdempotentClassifiesCredentialFailuresBeforeAndAfterAPossibleCommit(t *testing.T) {
	for _, testCase := range credentialClassificationCases {
		t.Run(testCase.name, func(t *testing.T) {
			database := newBalanceDatabase(scriptedmysql.Execution{AffectedRows: 1}, scriptedmysql.CommitAndReply)
			server, _ := startScriptedDatabase(t, database, scriptedmysql.Options{}, nil)
			client := newClientWithCredentials(t, server, erroringCredentials{err: testCase.err})
			step := newFencedStep()
			result, err := executeNonIdempotent(t, client, step, addToBalanceInput)
			if testCase.isTerminal {
				require.NoError(t, err)
				require.Equal(t, mysql.ExecuteStatementNonIdempotentBranchDefect, result.Branch, "terminal before any COMMIT")
			} else {
				var retry *sdkgo.RetryError
				require.ErrorAs(t, err, &retry, "transient before any COMMIT")
			}
			marker := lastMarker(t, step)
			require.Equal(t, singledispatch.StateNotSent, marker.State)

			step.AdvanceAttempt()
			require.NoError(t, step.SetPersistedCheckpoint(singledispatch.Marker{CallID: marker.CallID, Attempt: 2, State: singledispatch.StateCanResend, SendCount: 1}))
			step.AdvanceAttempt()
			result, err = executeNonIdempotent(t, client, step, addToBalanceInput)
			if testCase.isTerminal {
				require.NoError(t, err)
				require.Equal(t, mysql.ExecuteStatementNonIdempotentBranchUncertain, result.Branch, "terminal after a possible COMMIT")
			} else {
				var retry *sdkgo.RetryError
				require.ErrorAs(t, err, &retry, "transient after a possible COMMIT")
			}
			require.Empty(t, server.EventsOfKind(scriptedmysql.EventHandshake))
		})
	}
}

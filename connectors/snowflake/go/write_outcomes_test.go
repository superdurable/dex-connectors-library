// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package snowflake_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/snowflake/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const writeOutcomeHandle = "01b99999-0000-4000-8000-000000000003"

func newAnsweringClient(t *testing.T, status int, body string) *snowflake.Client {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, err := fmt.Fprint(w, body)
		require.NoError(t, err)
	}))
	t.Cleanup(server.Close)
	client, err := snowflake.New(snowflake.Config{AccountIdentifier: "myorg-analytics"},
		sdkgo.StaticCredentialProvider[snowflake.Credentials]{testConnection: tokenCredentials()},
		snowflake.WithLocalProviderURL(server.URL), snowflake.WithClock(func() time.Time { return fixedNow }))
	require.NoError(t, err)
	return client
}

func TestWritesRetryAStatusThatDoesNotProveARefusal(t *testing.T) {
	for _, status := range []int{http.StatusMethodNotAllowed, http.StatusRequestTimeout, http.StatusConflict, http.StatusTemporaryRedirect} {
		client := newAnsweringClient(t, status, `{}`)
		_, err := submit(newStep(), client, snowflake.SubmitStatementInput{Statement: "SELECT 1"})
		requireRetry(t, err, sdkgo.FailureAvailability)
		_, err = cancel(client, writeOutcomeHandle)
		requireRetry(t, err, sdkgo.FailureAvailability)
	}
}

func TestARepeatedCancelRefusedForAKnownHandleSelectsCanceled(t *testing.T) {
	refused := newAnsweringClient(t, http.StatusUnprocessableEntity, `{"code":"000605","sqlState":"01000","statementHandle":"`+writeOutcomeHandle+`"}`)
	repeatedStep := newStep()
	repeatedStep.AttemptNumber = 2
	repeated, err := sdkgo.RunMutation(repeatedStep, refused.CancelStatement(), testConnection, snowflake.CancelStatementInput{StatementHandle: writeOutcomeHandle})
	require.NoError(t, err)
	require.Equal(t, snowflake.CancelStatementBranchCanceled, repeated.Branch, "an earlier attempt may have canceled it")

	first, err := cancel(refused, writeOutcomeHandle)
	require.NoError(t, err)
	require.Equal(t, snowflake.CancelStatementBranchProviderRejected, first.Branch)

	unknown := newAnsweringClient(t, http.StatusUnprocessableEntity, `{"code":"000709","sqlState":"02000"}`)
	missingStep := newStep()
	missingStep.AttemptNumber = 2
	missing, err := sdkgo.RunMutation(missingStep, unknown.CancelStatement(), testConnection, snowflake.CancelStatementInput{StatementHandle: writeOutcomeHandle})
	require.NoError(t, err)
	require.Equal(t, snowflake.CancelStatementBranchProviderRejected, missing.Branch)
}

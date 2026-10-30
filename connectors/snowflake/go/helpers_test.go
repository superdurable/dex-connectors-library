// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package snowflake_test

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/snowflake"
	"github.com/superdurable/dex-connectors-library/connectors/snowflake/internal/fakesnowflake"
	"github.com/superdurable/dex-connectors-library/connectors/snowflake/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	testAccessToken = "pat-secret-value-0123456789"
	sentinelText    = "SENTINEL"
	cardLikeText    = "customer-card-4242"
)

var (
	testConnection = sdkgo.ConnectionRef{Provider: "snowflake", Name: "warehouse"}
	fixedNow       = time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	stepSequence   atomic.Int64

	sharedKeyOnce sync.Once
	sharedKey     *rsa.PrivateKey
)

// testPrivateKey returns one 2048-bit key per test binary, because generating keys is slow.
func testPrivateKey(t *testing.T) (*rsa.PrivateKey, string) {
	t.Helper()
	sharedKeyOnce.Do(func() {
		key, err := rsa.GenerateKey(rand.Reader, 2048)
		if err == nil {
			sharedKey = key
		}
	})
	require.NotNil(t, sharedKey)
	encoded, err := x509.MarshalPKCS8PrivateKey(sharedKey)
	require.NoError(t, err)
	return sharedKey, string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: encoded}))
}

// startFake runs a fake that accepts the test token and the test key for MYORG-ANALYTICS.DEX_SERVICE.
func startFake(t *testing.T, script func(fakesnowflake.SubmittedStatement) fakesnowflake.StatementScript) *fakesnowflake.Server {
	t.Helper()
	key, _ := testPrivateKey(t)
	return fakesnowflake.New(t, fakesnowflake.Credentials{
		ProgrammaticAccessToken: testAccessToken, KeyPairPublicKey: &key.PublicKey, QualifiedUserName: "MYORG-ANALYTICS.DEX_SERVICE",
	}, script)
}

func scriptAlways(script fakesnowflake.StatementScript) func(fakesnowflake.SubmittedStatement) fakesnowflake.StatementScript {
	return func(fakesnowflake.SubmittedStatement) fakesnowflake.StatementScript { return script }
}

func newTestClient(t *testing.T, fake *fakesnowflake.Server, credentials snowflake.Credentials, mutate func(*snowflake.Config)) *snowflake.Client {
	t.Helper()
	config := snowflake.Config{AccountIdentifier: "myorg-analytics", Warehouse: "REPORTING_WH", Role: "DEX_REPORTING", Database: "ANALYTICS", Schema: "BILLING"}
	if mutate != nil {
		mutate(&config)
	}
	client, err := snowflake.New(config, sdkgo.StaticCredentialProvider[snowflake.Credentials]{testConnection: credentials},
		snowflake.WithLocalProviderURL(fake.URL), snowflake.WithClock(func() time.Time { return fixedNow }))
	require.NoError(t, err)
	return client
}

func tokenCredentials() snowflake.Credentials {
	return snowflake.Credentials{AuthMethodID: snowflake.ProgrammaticAccessTokenAuthMethodID, ProgrammaticAccessToken: sdkgo.NewSecretString(testAccessToken)}
}

func keyPairCredentials(t *testing.T) snowflake.Credentials {
	t.Helper()
	_, privateKeyPEM := testPrivateKey(t)
	return snowflake.Credentials{AuthMethodID: snowflake.KeyPairAuthMethodID, User: "dex_service", PrivateKey: sdkgo.NewSecretString(privateKeyPEM)}
}

func newStep() *testsupport.DexContext {
	return testsupport.NewDexContext("snowflake-flow", fmt.Sprintf("step-%d", stepSequence.Add(1)))
}

func submit(step *testsupport.DexContext, client *snowflake.Client, input snowflake.SubmitStatementInput) (snowflake.SubmitStatementResult, error) {
	return sdkgo.RunMutation(step, client.SubmitStatement(), testConnection, input)
}

func readResult(client *snowflake.Client, input snowflake.GetStatementResultInput) (snowflake.GetStatementResultResult, error) {
	return sdkgo.RunQuery(newStep(), client.GetStatementResult(), testConnection, input)
}

func cancel(client *snowflake.Client, handle string) (snowflake.CancelStatementResult, error) {
	return sdkgo.RunMutation(newStep(), client.CancelStatement(), testConnection, snowflake.CancelStatementInput{StatementHandle: handle})
}

// requireProviderTextFree asserts a Result carries no provider message text, credential, or bound value.
func requireProviderTextFree(t *testing.T, result any) {
	t.Helper()
	encoded, err := json.Marshal(result)
	require.NoError(t, err)
	for _, forbidden := range []string{sentinelText, cardLikeText, testAccessToken, "PRIVATE KEY"} {
		require.NotContains(t, string(encoded), forbidden)
	}
}

func requireRetry(t *testing.T, err error, kind sdkgo.FailureKind) *sdkgo.RetryError {
	t.Helper()
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.Equal(t, kind, retry.Failure.Kind)
	return retry
}

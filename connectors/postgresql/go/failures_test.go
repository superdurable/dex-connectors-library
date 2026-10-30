// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package postgresql

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestClassifyFailureRoutesServerErrorsBySQLState(t *testing.T) {
	for code, want := range map[string]struct {
		disposition failureDisposition
		kind        sdkgo.FailureKind
	}{
		"40001": {dispositionRetry, sdkgo.FailureConflict},
		"40P01": {dispositionRetry, sdkgo.FailureConflict},
		"55P03": {dispositionRetry, sdkgo.FailureConflict},
		"57P01": {dispositionRetry, sdkgo.FailureAvailability},
		"57P03": {dispositionRetry, sdkgo.FailureAvailability},
		"53300": {dispositionRetry, sdkgo.FailureAvailability},
		"58030": {dispositionRetry, sdkgo.FailureAvailability},
		"08006": {dispositionRetry, sdkgo.FailureTransport},
		"28P01": {dispositionRejected, sdkgo.FailureAuthentication},
		"42501": {dispositionRejected, sdkgo.FailureAuthorization},
		"25006": {dispositionRejected, sdkgo.FailureAuthorization},
		"3D000": {dispositionRejected, sdkgo.FailureNotFound},
		"42P01": {dispositionRejected, sdkgo.FailureNotFound},
		"23505": {dispositionRejected, sdkgo.FailureConflict},
		"23503": {dispositionRejected, sdkgo.FailureConflict},
		"57014": {dispositionRejected, sdkgo.FailureAvailability},
		"22P02": {dispositionRejected, sdkgo.FailureValidation},
		"42601": {dispositionRejected, sdkgo.FailureValidation},
		"0A000": {dispositionRejected, sdkgo.FailureValidation},
		"XX000": {dispositionRejected, sdkgo.FailureProviderRejection},
	} {
		t.Run(code, func(t *testing.T) {
			failure := classifyFailure(fmt.Errorf("wrapped: %w", &pgconn.PgError{Code: code, Message: "contains a value: secret"}), phaseStatement)
			require.Equal(t, want.disposition, failure.disposition)
			require.Equal(t, want.kind, failure.kind)
			require.Equal(t, code, failure.sqlState)
			require.Contains(t, failure.message, "SQLSTATE "+code)
			require.NotContains(t, failure.message, "secret", "server message text never reaches a Failure")
		})
	}
}

func TestClassifyFailureTreatsOnlyAPossiblyReceivedCommitAsUncertain(t *testing.T) {
	require.Equal(t, dispositionUncertain, classifyFailure(io.ErrUnexpectedEOF, phaseCommit).disposition)
	require.Equal(t, dispositionUncertain, classifyFailure(context.DeadlineExceeded, phaseCommit).disposition)
	require.Equal(t, dispositionRetry, classifyFailure(io.ErrUnexpectedEOF, phaseStatement).disposition, "nothing commits before COMMIT")
	require.Equal(t, dispositionRetry, classifyFailure(io.ErrUnexpectedEOF, phaseBegin).disposition)
	require.Equal(t, dispositionRetry, classifyFailure(errCommitNotSent, phaseCommit).disposition)
	require.Equal(t, dispositionRejected, classifyFailure(errCommitRolledBack, phaseCommit).disposition)

	deferredConstraint := classifyFailure(&pgconn.PgError{Code: "23505", ConstraintName: "orders_pkey"}, phaseCommit)
	require.Equal(t, dispositionRejected, deferredConstraint.disposition, "a deferred constraint rejects COMMIT conclusively")
	require.Contains(t, deferredConstraint.message, `on constraint "orders_pkey"`)
	require.Equal(t, dispositionRetry, classifyFailure(&pgconn.PgError{Code: "40001"}, phaseCommit).disposition)
	terminated := classifyFailure(&pgconn.PgError{Code: "57P01"}, phaseCommit)
	require.Equal(t, dispositionUncertain, terminated.disposition, "a backend terminated while committing may already have committed locally")
	require.Equal(t, "57P01", terminated.sqlState)
}

func TestClassifyFailureSeparatesConclusiveConnectFailures(t *testing.T) {
	for name, err := range map[string]error{
		"unknown authority": x509.UnknownAuthorityError{},
		"host name":         x509.HostnameError{Certificate: &x509.Certificate{}, Host: "db.example.com"},
		"verification":      &tls.CertificateVerificationError{Err: errors.New("expired")},
	} {
		t.Run(name, func(t *testing.T) {
			failure := classifyFailure(fmt.Errorf("tls: %w", err), phaseConnect)
			require.Equal(t, dispositionRejected, failure.disposition)
			require.Equal(t, sdkgo.FailureAuthentication, failure.kind)
		})
	}
	refusedTLS := classifyFailure(errors.New("server refused TLS connection"), phaseConnect)
	require.Equal(t, dispositionRejected, refusedTLS.disposition)
	missingHost := classifyFailure(&net.DNSError{Err: "no such host", Name: "db.invalid", IsNotFound: true}, phaseConnect)
	require.Equal(t, sdkgo.FailureNotFound, missingHost.kind)
	temporaryLookup := classifyFailure(&net.DNSError{Err: "timeout", Name: "db.example.com", IsTimeout: true}, phaseConnect)
	require.Equal(t, dispositionRetry, temporaryLookup.disposition)
	refused := classifyFailure(&net.OpError{Op: "dial", Err: errors.New("connection refused")}, phaseConnect)
	require.Equal(t, dispositionRetry, refused.disposition)
	require.Equal(t, sdkgo.FailureAvailability, refused.kind)
	authentication := classifyFailure(&pgconn.PgError{Code: "28P01"}, phaseConnect)
	require.Equal(t, dispositionRejected, authentication.disposition)
}

func TestClassifyFailureMapsLocalErrors(t *testing.T) {
	undecodable := classifyFailure(&undecodableValueError{column: "flag", typeOID: boolTypeOID}, phaseStatement)
	require.Equal(t, dispositionInvalidResponse, undecodable.disposition)
	require.Equal(t, sdkgo.FailureProtocol, undecodable.kind)
	settings := classifyFailure(errConnectionSettingsInvalid, phaseConnect)
	require.Equal(t, dispositionDefect, settings.disposition)
}

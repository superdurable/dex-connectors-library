// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package sqlserver

import (
	"context"
	"crypto/x509"
	"errors"
	"io"
	"net"
	"testing"

	mssql "github.com/microsoft/go-mssqldb"
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestClassifyServerErrorUsesTheErrorNumberAndHidesTheMessage(t *testing.T) {
	for name, testCase := range map[string]struct {
		number      int32
		severity    uint8
		disposition failureDisposition
		kind        sdkgo.FailureKind
	}{
		"login failed":               {18456, 14, dispositionRejected, sdkgo.FailureAuthentication},
		"password expired":           {18487, 14, dispositionRejected, sdkgo.FailureAuthentication},
		"Azure SQL firewall":         {40615, 14, dispositionRejected, sdkgo.FailureAuthentication},
		"cannot open database":       {4060, 11, dispositionRejected, sdkgo.FailureNotFound},
		"permission denied":          {229, 14, dispositionRejected, sdkgo.FailureAuthorization},
		"read-only database":         {3906, 16, dispositionRejected, sdkgo.FailureAuthorization},
		"invalid object":             {208, 16, dispositionRejected, sdkgo.FailureNotFound},
		"unique constraint":          {2627, 14, dispositionRejected, sdkgo.FailureConflict},
		"unique index":               {2601, 14, dispositionRejected, sdkgo.FailureConflict},
		"foreign key or check":       {547, 16, dispositionRejected, sdkgo.FailureConflict},
		"null into not null":         {515, 16, dispositionRejected, sdkgo.FailureConflict},
		"truncation":                 {2628, 16, dispositionRejected, sdkgo.FailureValidation},
		"conversion":                 {245, 16, dispositionRejected, sdkgo.FailureValidation},
		"OUTPUT with triggers":       {334, 15, dispositionRejected, sdkgo.FailureValidation},
		"size quota":                 {40544, 20, dispositionRejected, sdkgo.FailureQuotaExhausted},
		"long-running transaction":   {40549, 16, dispositionRejected, sdkgo.FailureAvailability},
		"deadlock":                   {1205, 13, dispositionRetry, sdkgo.FailureConflict},
		"lock timeout":               {1222, 16, dispositionRetry, sdkgo.FailureConflict},
		"snapshot conflict":          {3960, 16, dispositionRetry, sdkgo.FailureConflict},
		"transaction log full":       {9002, 17, dispositionRetry, sdkgo.FailureAvailability},
		"Azure SQL service busy":     {40501, 20, dispositionRetry, sdkgo.FailureAvailability},
		"Azure SQL reconfiguration":  {40197, 17, dispositionRetry, sdkgo.FailureAvailability},
		"Azure SQL database offline": {40613, 17, dispositionRetry, sdkgo.FailureAvailability},
		"unknown fatal":              {50999, 21, dispositionRetry, sdkgo.FailureAvailability},
		"unknown ordinary":           {50000, 16, dispositionRejected, sdkgo.FailureProviderRejection},
	} {
		t.Run(name, func(t *testing.T) {
			failure := classifyFailure(mssql.Error{Number: testCase.number, State: 3, Class: testCase.severity, Message: "customer-card-4242"}, phaseStatement)
			require.Equal(t, testCase.disposition, failure.disposition)
			require.Equal(t, testCase.kind, failure.kind)
			require.Equal(t, testCase.number, failure.errorNumber)
			require.NotContains(t, failure.message, "customer-card-4242")
		})
	}
}

func TestClassifyServerErrorReadsOnlyPlainConstraintNames(t *testing.T) {
	for message, expected := range map[string]string{
		"Violation of PRIMARY KEY constraint 'pk_refunds'. Cannot insert duplicate key in object 'dbo.refunds'. The duplicate key value is (7).":                         ` on "pk_refunds"`,
		"Cannot insert duplicate key row in object 'dbo.refunds' with unique index 'ux_refunds_key'. The duplicate key value is (x).":                                    ` on "ux_refunds_key"`,
		`The INSERT statement conflicted with the CHECK constraint "ck_amount_positive". The conflict occurred in database "app", table "dbo.refunds", column 'amount'.`: ` on "ck_amount_positive"`,
		"Violation of UNIQUE KEY constraint 'name with ''quote'''. Cannot insert duplicate key in object 'dbo.refunds'.":                                                 "",
	} {
		number := int32(2627)
		switch {
		case message[0] == 'C':
			number = 2601
		case message[0] == 'T':
			number = 547
		}
		failure := classifyFailure(mssql.Error{Number: number, Class: 14, State: 1, Message: message}, phaseStatement)
		require.True(t, len(failure.message) > 0)
		if expected == "" {
			require.NotContains(t, failure.message, " on ")
			continue
		}
		require.Contains(t, failure.message, expected)
		require.NotContains(t, failure.message, "duplicate key value")
	}
}

func TestClassifyFailureTreatsOnlyAPossiblyReceivedCommitAsUncertain(t *testing.T) {
	require.Equal(t, dispositionRetry, classifyFailure(io.ErrUnexpectedEOF, phaseStatement).disposition, "nothing was committed before COMMIT")
	require.Equal(t, dispositionUncertain, classifyFailure(io.ErrUnexpectedEOF, phaseCommit).disposition)
	require.Equal(t, dispositionRetry, classifyFailure(errCommitNotSent, phaseCommit).disposition)
	require.Equal(t, dispositionUncertain, classifyFailure(mssql.Error{Number: 3999, Class: 17}, phaseCommit).disposition)
	require.Equal(t, dispositionRejected, classifyFailure(mssql.Error{Number: 3902, Class: 16}, phaseCommit).disposition)
	require.Equal(t, dispositionRetry, classifyFailure(mssql.Error{Number: 1205, Class: 13}, phaseCommit).disposition, "a deadlock victim rolled back")
	require.Equal(t, dispositionUncertain, classifyFailure(&driverPanicError{}, phaseCommit).disposition)
	require.Equal(t, dispositionInvalidResponse, classifyFailure(&driverPanicError{}, phaseStatement).disposition)
}

func TestClassifyFailureUnwrapsTheDriversStreamAndServerErrors(t *testing.T) {
	lost := classifyFailure(mssql.StreamError{InnerError: io.EOF}, phaseStatement)
	require.Equal(t, dispositionRetry, lost.disposition, "a stream error caused by a lost connection is retried")
	garbled := classifyFailure(mssql.StreamError{InnerError: errors.New("unknown token type")}, phaseStatement)
	require.Equal(t, dispositionInvalidResponse, garbled.disposition)
	retryable := classifyFailure(mssql.RetryableError{}, phaseStatement)
	require.Equal(t, dispositionRetry, retryable.disposition, "the driver's bad-connection wrapper is a lost connection")
	budget := classifyFailure(errors.Join(errServerReplyTooLarge, errReadBudgetExceeded), phaseBegin)
	require.Equal(t, dispositionInvalidResponse, budget.disposition)
	require.Equal(t, sdkgo.FailureResponseTooLarge, budget.kind)
	timeout := classifyFailure(errors.Join(errStatementTimedOut, context.DeadlineExceeded), phaseStatement)
	require.Equal(t, dispositionRejected, timeout.disposition)
}

func TestClassifyConnectFailuresSeparatesConclusiveFromTransient(t *testing.T) {
	require.Equal(t, dispositionRetry, classifyFailure(&net.OpError{Op: "dial", Err: errors.New("connection refused")}, phaseConnect).disposition)
	notFound := classifyFailure(&net.DNSError{Err: "no such host", Name: "db.example.com", IsNotFound: true}, phaseConnect)
	require.Equal(t, dispositionRejected, notFound.disposition)
	require.Equal(t, sdkgo.FailureNotFound, notFound.kind)
	refused := classifyFailure(errors.New("server does not support encryption"), phaseConnect)
	require.Equal(t, dispositionRejected, refused.disposition)
	certificate := classifyFailure(errors.Join(errCertificateRejected, errors.New("TLS Handshake failed: x509")), phaseConnect)
	require.Equal(t, sdkgo.FailureAuthentication, certificate.kind)
	unknownAuthority := classifyFailure(x509.UnknownAuthorityError{}, phaseConnect)
	require.Equal(t, sdkgo.FailureAuthentication, unknownAuthority.kind)
}

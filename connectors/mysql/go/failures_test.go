// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package mysql

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"testing"

	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func serverError(number uint16, sqlState string, message string) *mysqldriver.MySQLError {
	serverError := &mysqldriver.MySQLError{Number: number, Message: message}
	copy(serverError.SQLState[:], sqlState)
	return serverError
}

func TestClassifyFailureRoutesServerErrorsByNumberAndSQLState(t *testing.T) {
	for _, testCase := range []struct {
		number      uint16
		sqlState    string
		disposition failureDisposition
		kind        sdkgo.FailureKind
	}{
		{1213, "40001", dispositionRetry, sdkgo.FailureConflict},
		{1205, "HY000", dispositionRetry, sdkgo.FailureConflict},
		{1040, "08004", dispositionRetry, sdkgo.FailureAvailability},
		{1053, "08S01", dispositionRetry, sdkgo.FailureAvailability},
		{1927, "70100", dispositionRetry, sdkgo.FailureAvailability},
		{4031, "HY000", dispositionRetry, sdkgo.FailureAvailability},
		{1615, "HY000", dispositionRetry, sdkgo.FailureAvailability},
		{1158, "08S01", dispositionRetry, sdkgo.FailureAvailability},
		{1045, "28000", dispositionRejected, sdkgo.FailureAuthentication},
		{3159, "HY000", dispositionRejected, sdkgo.FailureAuthentication},
		{1862, "HY000", dispositionRejected, sdkgo.FailureAuthentication},
		{1044, "42000", dispositionRejected, sdkgo.FailureAuthorization},
		{1142, "42000", dispositionRejected, sdkgo.FailureAuthorization},
		{1792, "25006", dispositionRejected, sdkgo.FailureAuthorization},
		{1290, "HY000", dispositionRejected, sdkgo.FailureAuthorization},
		{1049, "42000", dispositionRejected, sdkgo.FailureNotFound},
		{1146, "42S02", dispositionRejected, sdkgo.FailureNotFound},
		{1054, "42S22", dispositionRejected, sdkgo.FailureNotFound},
		{1062, "23000", dispositionRejected, sdkgo.FailureConflict},
		{1452, "23000", dispositionRejected, sdkgo.FailureConflict},
		{3819, "HY000", dispositionRejected, sdkgo.FailureConflict},
		{4025, "23000", dispositionRejected, sdkgo.FailureConflict},
		{3024, "HY000", dispositionRejected, sdkgo.FailureAvailability},
		{1969, "70100", dispositionRejected, sdkgo.FailureAvailability},
		{1317, "70100", dispositionRejected, sdkgo.FailureAvailability},
		{1226, "42000", dispositionRejected, sdkgo.FailureQuotaExhausted},
		{1064, "42000", dispositionRejected, sdkgo.FailureValidation},
		{1366, "HY000", dispositionRejected, sdkgo.FailureValidation},
		{1406, "22001", dispositionRejected, sdkgo.FailureValidation},
		{1235, "42000", dispositionRejected, sdkgo.FailureValidation},
		{1105, "HY000", dispositionRejected, sdkgo.FailureProviderRejection},
	} {
		t.Run(strconv.Itoa(int(testCase.number)), func(t *testing.T) {
			failure := classifyFailure(fmt.Errorf("wrapped: %w", serverError(testCase.number, testCase.sqlState, "contains a value: secret")), phaseStatement)
			require.Equal(t, testCase.disposition, failure.disposition)
			require.Equal(t, testCase.kind, failure.kind)
			require.Equal(t, testCase.number, failure.errorNumber)
			require.Equal(t, testCase.sqlState, failure.sqlState)
			require.Contains(t, failure.message, "error "+strconv.Itoa(int(testCase.number)))
			require.NotContains(t, failure.message, "secret", "server message text never reaches a Failure")
		})
	}
}

func TestClassifyFailureNamesOnlyTheViolatedKeyOrConstraint(t *testing.T) {
	for _, testCase := range []struct {
		number  uint16
		message string
		want    string
	}{
		{1062, "Duplicate entry 'secret' for key 'refund_ledger.order_id'", `on "refund_ledger.order_id"`},
		{1062, "Duplicate entry 'secret' for key 'order_id'", `on "order_id"`},
		{3819, "Check constraint 'amount_positive' is violated.", `on "amount_positive"`},
		{4025, "CONSTRAINT `amount_positive` failed for `app`.`refund_ledger`", `on "amount_positive"`},
	} {
		failure := classifyFailure(serverError(testCase.number, "23000", testCase.message), phaseStatement)
		require.Contains(t, failure.message, testCase.want)
		require.NotContains(t, failure.message, "secret")
	}
	spoofed := classifyFailure(serverError(1062, "23000", "Duplicate entry 'x'' for key ''fake' for key 'it''s'"), phaseStatement)
	require.NotContains(t, spoofed.message, " on ", "a key name that is not a plain identifier is omitted")
}

func TestClassifyFailureTreatsOnlyAPossiblyReceivedCommitAsUncertain(t *testing.T) {
	require.Equal(t, dispositionUncertain, classifyFailure(io.ErrUnexpectedEOF, phaseCommit).disposition)
	require.Equal(t, dispositionUncertain, classifyFailure(mysqldriver.ErrInvalidConn, phaseCommit).disposition)
	require.Equal(t, dispositionUncertain, classifyFailure(context.DeadlineExceeded, phaseCommit).disposition)
	require.Equal(t, dispositionRetry, classifyFailure(mysqldriver.ErrInvalidConn, phaseStatement).disposition, "nothing commits before COMMIT")
	require.Equal(t, dispositionRetry, classifyFailure(io.ErrUnexpectedEOF, phaseBegin).disposition)
	require.Equal(t, dispositionRetry, classifyFailure(errCommitNotSent, phaseCommit).disposition)

	require.Equal(t, dispositionRetry, classifyFailure(serverError(1213, "40001", ""), phaseCommit).disposition, "a deadlock or Galera conflict at COMMIT rolled back")
	require.Equal(t, dispositionRetry, classifyFailure(serverError(1205, "HY000", ""), phaseCommit).disposition)
	require.Equal(t, dispositionRetry, classifyFailure(serverError(9999, "40001", ""), phaseCommit).disposition, "SQLSTATE 40001 is a rollback")
	require.Equal(t, dispositionRejected, classifyFailure(serverError(1062, "23000", ""), phaseCommit).disposition)
	groupReplication := classifyFailure(serverError(3101, "40000", ""), phaseCommit)
	require.Equal(t, dispositionUncertain, groupReplication.disposition, "an unlisted COMMIT error stays uncertain, the safe reading")
	duringCommit := classifyFailure(serverError(1180, "HY000", ""), phaseCommit)
	require.Equal(t, dispositionUncertain, duringCommit.disposition, "any other server error while committing leaves the outcome unknown")
	require.Equal(t, uint16(1180), duringCommit.errorNumber)
	require.Contains(t, duringCommit.message, "ER_ERROR_DURING_COMMIT")
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
	refusedTLS := classifyFailure(mysqldriver.ErrNoTLS, phaseConnect)
	require.Equal(t, dispositionRejected, refusedTLS.disposition)
	require.Contains(t, refusedTLS.message, "does not accept TLS")
	cleartext := classifyFailure(mysqldriver.ErrCleartextPassword, phaseConnect)
	require.Equal(t, sdkgo.FailureAuthentication, cleartext.kind)
	missingHost := classifyFailure(&net.DNSError{Err: "no such host", Name: "db.invalid", IsNotFound: true}, phaseConnect)
	require.Equal(t, sdkgo.FailureNotFound, missingHost.kind)
	temporaryLookup := classifyFailure(&net.DNSError{Err: "timeout", Name: "db.example.com", IsTimeout: true}, phaseConnect)
	require.Equal(t, dispositionRetry, temporaryLookup.disposition)
	refused := classifyFailure(&net.OpError{Op: "dial", Err: errors.New("connection refused")}, phaseConnect)
	require.Equal(t, dispositionRetry, refused.disposition)
	require.Equal(t, sdkgo.FailureAvailability, refused.kind)
	authentication := classifyFailure(serverError(1045, "28000", "Access denied for user 'dex_app'"), phaseConnect)
	require.Equal(t, dispositionRejected, authentication.disposition)
	require.NotContains(t, authentication.message, "dex_app")
}

func TestClassifyFailureRetriesOnlyAConnectionLossBeforeCommit(t *testing.T) {
	for name, err := range map[string]error{
		"invalid connection":   mysqldriver.ErrInvalidConn,
		"nothing written":      driver.ErrBadConn,
		"commands out of sync": mysqldriver.ErrPktSync,
		"call deadline":        context.DeadlineExceeded,
		"network":              &net.OpError{Op: "read", Err: errors.New("connection reset by peer")},
		"short write":          io.ErrShortWrite,
	} {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, dispositionRetry, classifyFailure(fmt.Errorf("wrapped: %w", err), phaseStatement).disposition)
		})
	}
	for name, err := range map[string]error{
		"unknown field type": errors.New("unknown field type 250"),
		"malformed packet":   mysqldriver.ErrMalformPkt,
		"text row number":    &strconv.NumError{Func: "ParseInt", Num: "x", Err: strconv.ErrSyntax},
	} {
		t.Run(name, func(t *testing.T) {
			failure := classifyFailure(err, phaseStatement)
			require.Equal(t, dispositionInvalidResponse, failure.disposition, "a reply that cannot be decoded will not decode on retry")
			require.Equal(t, sdkgo.FailureProtocol, failure.kind)
			require.NotContains(t, failure.message, "250")
		})
	}
	oversized := classifyFailure(errors.Join(errServerReplyTooLarge, mysqldriver.ErrInvalidConn), phaseBegin)
	require.Equal(t, dispositionInvalidResponse, oversized.disposition)
	require.Equal(t, sdkgo.FailureResponseTooLarge, oversized.kind)
	require.Equal(t, dispositionUncertain, classifyFailure(errors.Join(errServerReplyTooLarge, mysqldriver.ErrInvalidConn), phaseCommit).disposition,
		"an oversized COMMIT reply still means COMMIT was sent")
	require.Equal(t, dispositionDefect, classifyFailure(mysqldriver.ErrPktTooLarge, phaseStatement).disposition)
}

func TestClassifyFailureMapsLocalErrors(t *testing.T) {
	undecodable := classifyFailure(&undecodableValueError{column: "flag", typeName: "TINYINT"}, phaseStatement)
	require.Equal(t, dispositionInvalidResponse, undecodable.disposition)
	require.Equal(t, sdkgo.FailureProtocol, undecodable.kind)
	duplicate := classifyFailure(&duplicateColumnError{column: "id"}, phaseStatement)
	require.Equal(t, dispositionDefect, duplicate.disposition)
	require.Equal(t, dispositionDefect, classifyFailure(errConnectionSettingsInvalid, phaseConnect).disposition)
	require.Equal(t, dispositionDefect, classifyFailure(errTrustedRootsUnreadable, phaseConnect).disposition)
	require.Equal(t, dispositionInvalidResponse, classifyFailure(errStatementOutcomeInvalid, phaseStatement).disposition)
}

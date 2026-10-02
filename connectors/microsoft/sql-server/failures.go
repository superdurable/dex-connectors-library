// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package sqlserver

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"net"
	"regexp"
	"strings"

	mssql "github.com/microsoft/go-mssqldb"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

var (
	errConnectionSettingsInvalid = errors.New("SQL Server connection settings are invalid")
	errTrustedRootsUnreadable    = errors.New("the file named by the Worker's SQLSERVER_SSL_CA environment variable is unreadable or holds no PEM certificate")
	errDriverInterfaceMissing    = errors.New("the SQL Server driver connection does not implement a required database/sql/driver interface")
	errSessionIdentityInvalid    = errors.New("the server returned an unreadable @@SPID or product version")
	errCommitNotSent             = errors.New("the connection ended or the call deadline passed before COMMIT was sent, so nothing was committed")
	errServerReplyTooLarge       = errors.New("the server's reply exceeded the connector's read bound, so the connector closed the connection")
	errCertificateRejected       = errors.New("the SQL Server certificate failed encrypt verification")
	errStatementTimedOut         = errors.New("the statement exceeded statementTimeout, so the connector canceled it")
)

// driverPanicError replaces a driver panic, which a malformed server reply can cause, without its value.
type driverPanicError struct{}

// Error reports the panic without the recovered value, which may quote server data.
func (*driverPanicError) Error() string {
	return "the SQL Server driver could not process the server's reply"
}

// constraintNamePatterns read only a violated constraint's name from the us_english messages the session pins.
var constraintNamePatterns = map[int32]*regexp.Regexp{
	2627: regexp.MustCompile(`^Violation of (?:PRIMARY KEY|UNIQUE KEY) constraint '([A-Za-z0-9_$#@]{1,128})'\.`),
	2601: regexp.MustCompile(`with unique index '([A-Za-z0-9_$#@]{1,128})'\. The duplicate key value is`),
	547:  regexp.MustCompile(`^The (?:INSERT|UPDATE|DELETE|MERGE) statement conflicted with the [A-Z ]{1,40} constraint "([A-Za-z0-9_$#@]{1,128})"\.`),
}

// sessionPhase is the point in one connection's lifecycle where a failure occurred.
type sessionPhase int

const (
	phaseConnect sessionPhase = iota
	phaseBegin
	phaseStatement
	phaseCommit
)

// failureDisposition is how an operation reports one classified failure.
type failureDisposition int

const (
	dispositionRetry failureDisposition = iota
	dispositionRejected
	dispositionUncertain
	dispositionInvalidResponse
	dispositionDefect
)

// classifiedFailure is a secret-free description of one failed call.
type classifiedFailure struct {
	disposition   failureDisposition
	kind          sdkgo.FailureKind
	message       string
	errorNumber   int32
	errorState    uint8
	errorSeverity uint8
}

// errorNumberDescriptions follow Microsoft's error references; the README lists sources and unconfirmed numbers.
var errorNumberDescriptions = map[int32]string{
	102: "incorrect syntax", 137: "undeclared variable", 156: "incorrect syntax near a keyword",
	207: "invalid column name", 208: "invalid object name", 229: "permission denied on an object",
	230: "permission denied on a column", 241: "date or time conversion failed", 242: "out-of-range date or time conversion",
	245: "conversion failed", 262: "permission denied in the database", 266: "transaction count changed during the statement",
	297: "permission denied", 334: "OUTPUT clause without INTO on a table with enabled triggers",
	515: "NULL into a column that does not allow NULL", 547: "constraint conflict", 574: "statement cannot run inside a user transaction",
	615: "database ID not found", 701: "insufficient system memory", 916: "principal cannot access the database",
	926: "database marked suspect", 1101: "filegroup full", 1105: "filegroup full", 1205: "deadlock victim",
	1222: "lock request timeout", 2601: "duplicate key in a unique index", 2627: "unique or primary key constraint violation",
	2628: "string or binary data would be truncated", 2812: "stored procedure not found", 3902: "COMMIT without BEGIN TRANSACTION",
	3903: "ROLLBACK without BEGIN TRANSACTION", 3906: "database is read-only", 3930: "uncommittable transaction",
	3960: "snapshot isolation update conflict", 3961: "snapshot isolation conflict with a DDL statement",
	4060: "cannot open the requested database", 4064: "cannot open the user's default database", 4104: "multi-part identifier could not be bound",
	4221: "read-secondary login wait", 6005: "shutdown in progress", 8003: "too many parameters",
	8114: "data type conversion error", 8115: "arithmetic overflow", 8134: "divide by zero", 8152: "string or binary data would be truncated",
	8628: "query optimization timeout", 8645: "memory resource wait timeout", 8651: "memory grant unavailable",
	9002: "transaction log full", 10922: "operation failed; rerun the statement", 10928: "resource limit reached",
	10929: "resource guarantee unavailable", 10936: "elastic pool resource limit reached",
	18401: "server in script upgrade mode", 18452: "login from an untrusted domain", 18456: "login failed",
	18470: "account disabled", 18486: "account locked out", 18487: "password expired", 18488: "password must be changed",
	40197: "service error processing the request", 40501: "service busy", 40532: "login failed (Azure SQL)",
	40544: "database size quota reached", 40549: "session terminated for a long-running transaction",
	40550: "session terminated for too many locks", 40551: "session terminated for tempdb usage",
	40552: "session terminated for transaction log usage", 40553: "session terminated for memory usage",
	40613: "database not currently available", 40615: "login failed (Azure SQL)", 49918: "not enough resources",
	49919: "too many create or update operations", 49920: "too many operations",
}

var (
	retryableConflictNumbers     = numberSet(1205, 1222, 3960, 3961)
	retryableAvailabilityNumbers = numberSet(615, 701, 926, 1101, 1105, 4221, 6005, 8628, 8645, 8651, 9002, 10922, 10928, 10929, 10936,
		18401, 40197, 40501, 40613, 49918, 49919, 49920)
	authenticationNumbers = numberSet(18452, 18456, 18470, 18486, 18487, 18488, 40532, 40615)
	authorizationNumbers  = numberSet(229, 230, 262, 297, 916, 3906)
	notFoundNumbers       = numberSet(207, 208, 2812, 4060, 4064, 4104)
	conflictNumbers       = numberSet(515, 547, 2601, 2627)
	validationNumbers     = numberSet(102, 137, 156, 241, 242, 245, 266, 334, 574, 2628, 8003, 8114, 8115, 8134, 8152)
	quotaNumbers          = numberSet(40544)
	sessionLimitNumbers   = numberSet(40549, 40550, 40551, 40552, 40553)
	// commitRolledBackNumbers prove a failed COMMIT committed nothing: the server reports no open transaction or a deadlock.
	commitRolledBackNumbers = numberSet(1205, 3902, 3903)
)

// fatalSeverity is the lowest severity at which SQL Server ends the connection.
const fatalSeverity = 20

// classifyFailure treats only a possibly received COMMIT as uncertain; earlier failures committed nothing.
func classifyFailure(err error, phase sessionPhase) classifiedFailure {
	var serverError mssql.Error
	if errors.As(err, &serverError) {
		return classifyServerError(serverError, phase)
	}
	var undecodable *undecodableValueError
	var shape *resultShapeError
	switch {
	case errors.As(err, &undecodable):
		return classifiedFailure{disposition: dispositionInvalidResponse, kind: sdkgo.FailureProtocol, message: undecodable.Error()}
	case errors.As(err, &shape):
		return classifiedFailure{disposition: dispositionDefect, kind: sdkgo.FailureValidation, message: shape.Error()}
	case errors.Is(err, errCertificateRejected):
		return classifiedFailure{disposition: dispositionRejected, kind: sdkgo.FailureAuthentication, message: errCertificateRejected.Error()}
	case errors.Is(err, errSessionIdentityInvalid):
		return classifiedFailure{disposition: dispositionInvalidResponse, kind: sdkgo.FailureProtocol, message: err.Error()}
	case errors.Is(err, errConnectionSettingsInvalid) || errors.Is(err, errTrustedRootsUnreadable) || errors.Is(err, errDriverInterfaceMissing):
		return classifiedFailure{disposition: dispositionDefect, kind: sdkgo.FailureValidation, message: err.Error()}
	case errors.Is(err, errCommitNotSent):
		return classifiedFailure{disposition: dispositionRetry, kind: sdkgo.FailureTransport, message: errCommitNotSent.Error()}
	case errors.Is(err, errStatementTimedOut) && phase == phaseStatement:
		return classifiedFailure{
			disposition: dispositionRejected, kind: sdkgo.FailureAvailability,
			message: errStatementTimedOut.Error() + "; nothing was committed",
		}
	case errors.Is(err, errServerReplyTooLarge) && phase != phaseCommit:
		return classifiedFailure{disposition: dispositionInvalidResponse, kind: sdkgo.FailureResponseTooLarge, message: errServerReplyTooLarge.Error()}
	}
	if phase == phaseConnect {
		if failure, isConclusive := classifyConclusiveConnectFailure(err); isConclusive {
			return failure
		}
		return classifiedFailure{disposition: dispositionRetry, kind: sdkgo.FailureAvailability, message: "the SQL Server is unreachable or did not finish connecting in time"}
	}
	if phase == phaseCommit {
		return classifiedFailure{
			disposition: dispositionUncertain, kind: sdkgo.FailureTransport,
			message: "the connection failed after COMMIT was sent, so whether the transaction committed is unknown",
		}
	}
	if !isConnectionLoss(err) {
		return classifiedFailure{
			disposition: dispositionInvalidResponse, kind: sdkgo.FailureProtocol,
			message: fmt.Sprintf("the driver could not decode the server's reply during %s; nothing was committed", phase.description()),
		}
	}
	return classifiedFailure{
		disposition: dispositionRetry, kind: sdkgo.FailureTransport,
		message: fmt.Sprintf("the connection failed during %s before anything was committed", phase.description()),
	}
}

// isConnectionLoss reports a failure a new connection can cure, as opposed to a reply the driver cannot decode.
func isConnectionLoss(err error) bool {
	var streamError mssql.StreamError
	if errors.As(err, &streamError) {
		// The driver's stream error does not unwrap, so inspect the read failure it carries.
		err = streamError.InnerError
	}
	var networkError net.Error
	return errors.Is(err, driver.ErrBadConn) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.ErrShortWrite) ||
		errors.Is(err, net.ErrClosed) || errors.Is(err, errReadBudgetExceeded) || errors.As(err, &networkError)
}

func classifyServerError(serverError mssql.Error, phase sessionPhase) classifiedFailure {
	number := serverError.Number
	failure := classifiedFailure{
		disposition: dispositionRejected, kind: sdkgo.FailureProviderRejection,
		errorNumber: number, errorState: serverError.State, errorSeverity: serverError.Class,
	}
	if phase == phaseCommit && !commitRolledBackNumbers[number] {
		failure.disposition, failure.kind = dispositionUncertain, sdkgo.FailureAvailability
		failure.message = fmt.Sprintf("the server returned %s while committing, so whether the transaction committed is unknown", describeServerError(serverError))
		return failure
	}
	switch {
	case retryableConflictNumbers[number]:
		failure.disposition, failure.kind = dispositionRetry, sdkgo.FailureConflict
	case retryableAvailabilityNumbers[number]:
		failure.disposition, failure.kind = dispositionRetry, sdkgo.FailureAvailability
	case number == 3902 || number == 3903:
		failure.kind = sdkgo.FailureConflict
	case authenticationNumbers[number]:
		failure.kind = sdkgo.FailureAuthentication
	case authorizationNumbers[number]:
		failure.kind = sdkgo.FailureAuthorization
	case notFoundNumbers[number]:
		failure.kind = sdkgo.FailureNotFound
	case conflictNumbers[number]:
		failure.kind = sdkgo.FailureConflict
	case validationNumbers[number]:
		failure.kind = sdkgo.FailureValidation
	case quotaNumbers[number]:
		failure.kind = sdkgo.FailureQuotaExhausted
	case sessionLimitNumbers[number]:
		failure.kind = sdkgo.FailureAvailability
	case serverError.Class >= fatalSeverity:
		failure.disposition, failure.kind = dispositionRetry, sdkgo.FailureAvailability
	}
	failure.message = fmt.Sprintf("the server returned %s during %s", describeServerError(serverError), phase.description())
	if pattern, hasPattern := constraintNamePatterns[number]; hasPattern {
		if match := pattern.FindStringSubmatch(serverError.Message); match != nil {
			failure.message += fmt.Sprintf(" on %q", match[1])
		}
	}
	return failure
}

func classifyConclusiveConnectFailure(err error) (classifiedFailure, bool) {
	var unknownAuthority x509.UnknownAuthorityError
	var hostnameMismatch x509.HostnameError
	var invalidCertificate x509.CertificateInvalidError
	var verificationFailure *tls.CertificateVerificationError
	if errors.As(err, &unknownAuthority) || errors.As(err, &hostnameMismatch) ||
		errors.As(err, &invalidCertificate) || errors.As(err, &verificationFailure) {
		return classifiedFailure{disposition: dispositionRejected, kind: sdkgo.FailureAuthentication, message: errCertificateRejected.Error()}, true
	}
	// These texts are the driver's own messages, which carry no server data.
	switch message := err.Error(); {
	case strings.Contains(message, "server does not support encryption"):
		return classifiedFailure{
			disposition: dispositionRejected, kind: sdkgo.FailureProviderRejection,
			message: "the SQL Server does not accept TLS, which encrypt requires",
		}, true
	case strings.Contains(message, "federated authentication is not supported"):
		return classifiedFailure{disposition: dispositionRejected, kind: sdkgo.FailureProviderRejection, message: "the SQL Server refused the login method"}, true
	}
	var lookupFailure *net.DNSError
	if errors.As(err, &lookupFailure) && lookupFailure.IsNotFound {
		return classifiedFailure{disposition: dispositionRejected, kind: sdkgo.FailureNotFound, message: "the SQL Server host name does not resolve"}, true
	}
	var panicFailure *driverPanicError
	if errors.As(err, &panicFailure) {
		return classifiedFailure{disposition: dispositionInvalidResponse, kind: sdkgo.FailureProtocol, message: panicFailure.Error() + " while connecting"}, true
	}
	return classifiedFailure{}, false
}

func (phase sessionPhase) description() string {
	switch phase {
	case phaseConnect:
		return "connect and login"
	case phaseBegin:
		return "session setup and BEGIN TRANSACTION"
	case phaseCommit:
		return "COMMIT"
	default:
		return "the statement"
	}
}

// describeServerError names the number, severity, and state; it never repeats the server's message, which can quote values.
func describeServerError(serverError mssql.Error) string {
	description := fmt.Sprintf("error %d", serverError.Number)
	if name, isKnown := errorNumberDescriptions[serverError.Number]; isKnown {
		description += " (" + name + ")"
	}
	return description + fmt.Sprintf(", severity %d, state %d", serverError.Class, serverError.State)
}

func numberSet(numbers ...int32) map[int32]bool {
	set := make(map[int32]bool, len(numbers))
	for _, number := range numbers {
		set[number] = true
	}
	return set
}

func sdkFailure(operationID string, failure classifiedFailure) sdkgo.Failure {
	return *failurePointer(operationID, failure)
}

func failurePointer(operationID string, failure classifiedFailure) *sdkgo.Failure {
	value := sdkgo.Failure{Kind: failure.kind, Provider: ConnectorID, Operation: operationID, Message: failure.message}
	return &value
}

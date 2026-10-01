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
	"regexp"
	"strings"

	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

var (
	errConnectionSettingsInvalid = errors.New("MySQL connection settings are invalid")
	errTrustedRootsUnreadable    = errors.New("the file named by the Worker's MYSQL_SSL_CA environment variable is unreadable or holds no PEM certificate")
	errDriverInterfaceMissing    = errors.New("the MySQL driver connection does not implement a required database/sql/driver interface")
	errSessionIdentityInvalid    = errors.New("the server returned an unreadable CONNECTION_ID() or VERSION()")
	errStatementOutcomeInvalid   = errors.New("the server returned an unreadable ROW_COUNT(), LAST_INSERT_ID(), or @@warning_count")
	errCommitNotSent             = errors.New("the connection ended or the call deadline passed before COMMIT was sent, so nothing was committed")
	errServerReplyTooLarge       = errors.New("the server's reply exceeded the connector's read bound, so the connector closed the connection")
)

// constraintNamePatterns read only the trailing identifier of messages that name a violated key or constraint.
var constraintNamePatterns = map[uint16]*regexp.Regexp{
	1062: regexp.MustCompile(`for key '([A-Za-z0-9_$.]{1,192})'$`),
	3819: regexp.MustCompile(`^Check constraint '([A-Za-z0-9_$]{1,64})' is violated\.$`),
	4025: regexp.MustCompile("^CONSTRAINT `([A-Za-z0-9_$]{1,64})` failed for "),
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
	disposition failureDisposition
	kind        sdkgo.FailureKind
	message     string
	errorNumber uint16
	sqlState    string
}

// errorNumberNames names common server error numbers so Failure messages are readable without server text.
var errorNumberNames = map[uint16]string{
	1021: "ER_DISK_FULL", 1037: "ER_OUTOFMEMORY", 1040: "ER_CON_COUNT_ERROR", 1041: "ER_OUT_OF_RESOURCES",
	1044: "ER_DBACCESS_DENIED_ERROR", 1045: "ER_ACCESS_DENIED_ERROR", 1048: "ER_BAD_NULL_ERROR", 1049: "ER_BAD_DB_ERROR",
	1053: "ER_SERVER_SHUTDOWN", 1054: "ER_BAD_FIELD_ERROR", 1062: "ER_DUP_ENTRY", 1064: "ER_PARSE_ERROR",
	1109: "ER_UNKNOWN_TABLE", 1129: "ER_HOST_IS_BLOCKED", 1130: "ER_HOST_NOT_PRIVILEGED", 1142: "ER_TABLEACCESS_DENIED_ERROR",
	1143: "ER_COLUMNACCESS_DENIED_ERROR", 1146: "ER_NO_SUCH_TABLE", 1158: "ER_NET_READ_ERROR", 1159: "ER_NET_READ_INTERRUPTED",
	1160: "ER_NET_ERROR_ON_WRITE", 1161: "ER_NET_WRITE_INTERRUPTED", 1180: "ER_ERROR_DURING_COMMIT",
	1203: "ER_TOO_MANY_USER_CONNECTIONS", 1205: "ER_LOCK_WAIT_TIMEOUT", 1213: "ER_LOCK_DEADLOCK", 1216: "ER_NO_REFERENCED_ROW",
	1217: "ER_ROW_IS_REFERENCED", 1226: "ER_USER_LIMIT_REACHED", 1227: "ER_SPECIFIC_ACCESS_DENIED_ERROR",
	1235: "ER_NOT_SUPPORTED_YET", 1251: "ER_NOT_SUPPORTED_AUTH_MODE", 1264: "ER_WARN_DATA_OUT_OF_RANGE",
	1290: "ER_OPTION_PREVENTS_STATEMENT", 1292: "ER_TRUNCATED_WRONG_VALUE", 1295: "ER_UNSUPPORTED_PS",
	1305: "ER_SP_DOES_NOT_EXIST", 1317: "ER_QUERY_INTERRUPTED", 1365: "ER_DIVISION_BY_ZERO",
	1366: "ER_TRUNCATED_WRONG_VALUE_FOR_FIELD", 1370: "ER_PROCACCESS_DENIED_ERROR", 1406: "ER_DATA_TOO_LONG",
	1451: "ER_ROW_IS_REFERENCED_2", 1452: "ER_NO_REFERENCED_ROW_2", 1615: "ER_NEED_REPREPARE",
	1637: "ER_TOO_MANY_CONCURRENT_TRXS", 1698: "ER_ACCESS_DENIED_NO_PASSWORD_ERROR",
	1792: "ER_CANT_EXECUTE_IN_READ_ONLY_TRANSACTION", 1820: "ER_MUST_CHANGE_PASSWORD", 1836: "ER_READ_ONLY_MODE",
	1862: "ER_MUST_CHANGE_PASSWORD_LOGIN", 1927: "ER_CONNECTION_KILLED", 1969: "ER_STATEMENT_TIMEOUT",
	3024: "ER_QUERY_TIMEOUT", 3118: "ER_ACCOUNT_HAS_BEEN_LOCKED", 3140: "ER_INVALID_JSON_TEXT",
	3159: "ER_SECURE_TRANSPORT_REQUIRED", 3819: "ER_CHECK_CONSTRAINT_VIOLATED", 4025: "ER_CONSTRAINT_FAILED",
	4031: "ER_CLIENT_INTERACTION_TIMEOUT", 4151: "ER_ACCOUNT_HAS_BEEN_LOCKED",
}

var (
	retryableConflictNumbers     = numberSet(1205, 1213)
	retryableAvailabilityNumbers = numberSet(1021, 1037, 1040, 1041, 1053, 1158, 1159, 1160, 1161, 1203, 1615, 1637, 1927, 4031)
	authenticationNumbers        = numberSet(1045, 1129, 1130, 1251, 1698, 1820, 1862, 3118, 3159, 4151)
	authorizationNumbers         = numberSet(1044, 1142, 1143, 1227, 1290, 1370, 1792, 1836)
	notFoundNumbers              = numberSet(1049, 1054, 1109, 1146, 1305)
	conflictNumbers              = numberSet(3819, 4025)
	statementTimeoutNumbers      = numberSet(1317, 1969, 3024)
	validationNumbers            = numberSet(1235, 1295, 1366)
)

// classifyFailure treats only a possibly received COMMIT as uncertain; earlier failures committed nothing.
func classifyFailure(err error, phase sessionPhase) classifiedFailure {
	var serverError *mysqldriver.MySQLError
	if errors.As(err, &serverError) {
		return classifyServerError(serverError, phase)
	}
	var undecodable *undecodableValueError
	var duplicateColumn *duplicateColumnError
	switch {
	case errors.As(err, &undecodable):
		return classifiedFailure{disposition: dispositionInvalidResponse, kind: sdkgo.FailureProtocol, message: undecodable.Error()}
	case errors.As(err, &duplicateColumn):
		return classifiedFailure{disposition: dispositionDefect, kind: sdkgo.FailureValidation, message: duplicateColumn.Error()}
	case errors.Is(err, errSessionIdentityInvalid) || errors.Is(err, errStatementOutcomeInvalid):
		return classifiedFailure{disposition: dispositionInvalidResponse, kind: sdkgo.FailureProtocol, message: err.Error()}
	case errors.Is(err, errConnectionSettingsInvalid) || errors.Is(err, errTrustedRootsUnreadable) || errors.Is(err, errDriverInterfaceMissing):
		return classifiedFailure{disposition: dispositionDefect, kind: sdkgo.FailureValidation, message: err.Error()}
	case errors.Is(err, errCommitNotSent):
		return classifiedFailure{disposition: dispositionRetry, kind: sdkgo.FailureTransport, message: errCommitNotSent.Error()}
	case errors.Is(err, mysqldriver.ErrPktTooLarge):
		return classifiedFailure{disposition: dispositionDefect, kind: sdkgo.FailureValidation, message: "the statement and its parameters exceed the driver's packet limit"}
	case errors.Is(err, errServerReplyTooLarge) && phase != phaseCommit:
		return classifiedFailure{disposition: dispositionInvalidResponse, kind: sdkgo.FailureResponseTooLarge, message: errServerReplyTooLarge.Error()}
	}
	if phase == phaseConnect {
		if failure, isConclusive := classifyConclusiveConnectFailure(err); isConclusive {
			return failure
		}
		return classifiedFailure{disposition: dispositionRetry, kind: sdkgo.FailureAvailability, message: "the MySQL server is unreachable or did not finish connecting in time"}
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
	var networkError net.Error
	return errors.Is(err, mysqldriver.ErrInvalidConn) || errors.Is(err, driver.ErrBadConn) || errors.Is(err, mysqldriver.ErrPktSync) ||
		errors.Is(err, mysqldriver.ErrPktSyncMul) || errors.Is(err, mysqldriver.ErrBusyBuffer) || errors.Is(err, context.Canceled) ||
		errors.Is(err, context.DeadlineExceeded) || errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) ||
		errors.Is(err, io.ErrShortWrite) || errors.As(err, &networkError)
}

func classifyServerError(serverError *mysqldriver.MySQLError, phase sessionPhase) classifiedFailure {
	number := serverError.Number
	sqlState := strings.TrimRight(string(serverError.SQLState[:]), "\x00")
	failure := classifiedFailure{disposition: dispositionRejected, kind: sdkgo.FailureProviderRejection, errorNumber: number, sqlState: sqlState}
	// Only a deadlock, a Galera certification conflict, or a constraint error at COMMIT proves the transaction rolled back.
	if phase == phaseCommit && !retryableConflictNumbers[number] && sqlState != "40001" && !strings.HasPrefix(sqlState, "23") {
		failure.disposition, failure.kind = dispositionUncertain, sdkgo.FailureAvailability
		failure.message = fmt.Sprintf("the server returned %s while committing, so whether the transaction committed is unknown", describeServerError(number, sqlState))
		return failure
	}
	switch {
	case retryableConflictNumbers[number] || sqlState == "40001":
		failure.disposition, failure.kind = dispositionRetry, sdkgo.FailureConflict
	case retryableAvailabilityNumbers[number]:
		failure.disposition, failure.kind = dispositionRetry, sdkgo.FailureAvailability
	case number == 1226:
		failure.kind = sdkgo.FailureQuotaExhausted
	case strings.HasPrefix(sqlState, "08"):
		failure.disposition, failure.kind = dispositionRetry, sdkgo.FailureTransport
	case authenticationNumbers[number] || strings.HasPrefix(sqlState, "28"):
		failure.kind = sdkgo.FailureAuthentication
	case authorizationNumbers[number] || sqlState == "25006":
		failure.kind = sdkgo.FailureAuthorization
	case notFoundNumbers[number] || strings.HasPrefix(sqlState, "42S"):
		failure.kind = sdkgo.FailureNotFound
	case conflictNumbers[number] || strings.HasPrefix(sqlState, "23"):
		failure.kind = sdkgo.FailureConflict
	case statementTimeoutNumbers[number]:
		failure.kind = sdkgo.FailureAvailability
	case validationNumbers[number] || strings.HasPrefix(sqlState, "22") || strings.HasPrefix(sqlState, "42") || strings.HasPrefix(sqlState, "0A"):
		failure.kind = sdkgo.FailureValidation
	}
	failure.message = fmt.Sprintf("the server returned %s during %s", describeServerError(number, sqlState), phase.description())
	if statementTimeoutNumbers[number] {
		failure.message += "; the statement exceeded statementTimeout or was interrupted"
	}
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
		return classifiedFailure{
			disposition: dispositionRejected, kind: sdkgo.FailureAuthentication,
			message: "the MySQL server certificate failed sslMode verification",
		}, true
	}
	switch {
	case errors.Is(err, mysqldriver.ErrNoTLS):
		return classifiedFailure{
			disposition: dispositionRejected, kind: sdkgo.FailureProviderRejection,
			message: "the MySQL server does not accept TLS, which sslMode requires",
		}, true
	case errors.Is(err, mysqldriver.ErrCleartextPassword) || errors.Is(err, mysqldriver.ErrOldPassword) ||
		errors.Is(err, mysqldriver.ErrNativePassword) || errors.Is(err, mysqldriver.ErrUnknownPlugin):
		return classifiedFailure{
			disposition: dispositionRejected, kind: sdkgo.FailureAuthentication,
			message: "the account requires an authentication plugin the connector does not enable; use caching_sha2_password, mysql_native_password, or client_ed25519",
		}, true
	}
	var lookupFailure *net.DNSError
	if errors.As(err, &lookupFailure) && lookupFailure.IsNotFound {
		return classifiedFailure{disposition: dispositionRejected, kind: sdkgo.FailureNotFound, message: "the MySQL host name does not resolve"}, true
	}
	return classifiedFailure{}, false
}

func (phase sessionPhase) description() string {
	switch phase {
	case phaseConnect:
		return "connect"
	case phaseBegin:
		return "session setup and START TRANSACTION"
	case phaseCommit:
		return "COMMIT"
	default:
		return "the statement"
	}
}

func describeServerError(number uint16, sqlState string) string {
	description := fmt.Sprintf("error %d", number)
	name, isKnown := errorNumberNames[number]
	switch {
	case isKnown && sqlState != "":
		description += " (" + name + ", SQLSTATE " + sqlState + ")"
	case isKnown:
		description += " (" + name + ")"
	case sqlState != "":
		description += " (SQLSTATE " + sqlState + ")"
	}
	return description
}

func numberSet(numbers ...uint16) map[uint16]bool {
	set := make(map[uint16]bool, len(numbers))
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

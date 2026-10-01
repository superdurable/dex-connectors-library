// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package postgresql

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

var (
	errConnectionSettingsInvalid = errors.New("PostgreSQL connection settings are invalid")
	errCommitRolledBack          = errors.New("PostgreSQL rolled back the transaction at COMMIT")
	errCommitNotSent             = errors.New("the connection ended or the call deadline passed before COMMIT was sent, so nothing was committed")
)

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
	sqlState    string
}

// sqlStateConditionNames names common SQLSTATE codes so Failure messages are readable without server text.
var sqlStateConditionNames = map[string]string{
	"08000": "connection_exception", "08003": "connection_does_not_exist", "08006": "connection_failure",
	"0A000": "feature_not_supported", "22001": "string_data_right_truncation", "22003": "numeric_value_out_of_range",
	"22007": "invalid_datetime_format", "22008": "datetime_field_overflow", "22012": "division_by_zero",
	"22021": "character_not_in_repertoire", "22P02": "invalid_text_representation", "23502": "not_null_violation",
	"23503": "foreign_key_violation", "23505": "unique_violation", "23514": "check_violation",
	"23P01": "exclusion_violation", "25001": "active_sql_transaction", "25006": "read_only_sql_transaction",
	"28000": "invalid_authorization_specification", "28P01": "invalid_password", "3D000": "invalid_catalog_name",
	"3F000": "invalid_schema_name", "40001": "serialization_failure", "40P01": "deadlock_detected",
	"42501": "insufficient_privilege", "42601": "syntax_error", "42703": "undefined_column",
	"42704": "undefined_object", "42804": "datatype_mismatch", "42883": "undefined_function",
	"42P01": "undefined_table", "42P18": "indeterminate_datatype", "53100": "disk_full", "53200": "out_of_memory",
	"53300": "too_many_connections", "55P03": "lock_not_available", "57014": "query_canceled",
	"57P01": "admin_shutdown", "57P02": "crash_shutdown", "57P03": "cannot_connect_now",
}

// classifyFailure treats only a possibly received COMMIT as uncertain; earlier failures committed nothing.
func classifyFailure(err error, phase sessionPhase) classifiedFailure {
	var serverError *pgconn.PgError
	if errors.As(err, &serverError) {
		return classifyServerError(serverError, phase)
	}
	var undecodable *undecodableValueError
	switch {
	case errors.As(err, &undecodable):
		return classifiedFailure{disposition: dispositionInvalidResponse, kind: sdkgo.FailureProtocol, message: undecodable.Error()}
	case errors.Is(err, errConnectionSettingsInvalid):
		return classifiedFailure{disposition: dispositionDefect, kind: sdkgo.FailureValidation, message: err.Error()}
	case errors.Is(err, errCommitRolledBack):
		return classifiedFailure{disposition: dispositionRejected, kind: sdkgo.FailureProviderRejection, message: err.Error()}
	case errors.Is(err, errCommitNotSent):
		return classifiedFailure{disposition: dispositionRetry, kind: sdkgo.FailureTransport, message: err.Error()}
	}
	if phase == phaseConnect {
		if failure, isConclusive := classifyConclusiveConnectFailure(err); isConclusive {
			return failure
		}
		return classifiedFailure{disposition: dispositionRetry, kind: sdkgo.FailureAvailability, message: "PostgreSQL is unreachable or did not finish connecting in time"}
	}
	if phase == phaseCommit {
		return classifiedFailure{
			disposition: dispositionUncertain, kind: sdkgo.FailureTransport,
			message: "the connection failed after COMMIT was sent, so whether the transaction committed is unknown",
		}
	}
	return classifiedFailure{
		disposition: dispositionRetry, kind: sdkgo.FailureTransport,
		message: fmt.Sprintf("the connection failed during %s before anything was committed", phase.description()),
	}
}

func classifyServerError(serverError *pgconn.PgError, phase sessionPhase) classifiedFailure {
	code := serverError.Code
	failure := classifiedFailure{disposition: dispositionRejected, kind: sdkgo.FailureProviderRejection, sqlState: code}
	// A terminated backend or canceled synchronous-replication wait can report an error after committing locally.
	if phase == phaseCommit && !strings.HasPrefix(code, "23") && !strings.HasPrefix(code, "40") {
		failure.disposition, failure.kind = dispositionUncertain, sdkgo.FailureAvailability
		failure.message = fmt.Sprintf("PostgreSQL returned SQLSTATE %s while committing, so whether the transaction committed is unknown", describeSQLState(code))
		return failure
	}
	switch {
	case code == "40001" || code == "40P01" || code == "55P03":
		failure.disposition, failure.kind = dispositionRetry, sdkgo.FailureConflict
	case code == "57P01" || code == "57P02" || code == "57P03" || strings.HasPrefix(code, "53") || strings.HasPrefix(code, "58"):
		failure.disposition, failure.kind = dispositionRetry, sdkgo.FailureAvailability
	case strings.HasPrefix(code, "08"):
		failure.disposition, failure.kind = dispositionRetry, sdkgo.FailureTransport
	case code == "28000" || code == "28P01":
		failure.kind = sdkgo.FailureAuthentication
	case code == "42501" || code == "25006":
		failure.kind = sdkgo.FailureAuthorization
	case code == "3D000" || code == "3F000" || code == "42P01" || code == "42703" || code == "42704" || code == "42883":
		failure.kind = sdkgo.FailureNotFound
	case strings.HasPrefix(code, "23"):
		failure.kind = sdkgo.FailureConflict
	case code == "57014":
		failure.kind = sdkgo.FailureAvailability
	case strings.HasPrefix(code, "22") || strings.HasPrefix(code, "42") || strings.HasPrefix(code, "0A"):
		failure.kind = sdkgo.FailureValidation
	}
	failure.message = fmt.Sprintf("PostgreSQL returned SQLSTATE %s during %s", describeSQLState(code), phase.description())
	if code == "57014" {
		failure.message += "; the statement exceeded statementTimeout or was canceled"
	}
	if serverError.ConstraintName != "" && isSafeIdentifier(serverError.ConstraintName) {
		failure.message += fmt.Sprintf(" on constraint %q", serverError.ConstraintName)
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
			message: "the PostgreSQL server certificate failed sslMode verification",
		}, true
	}
	if strings.Contains(err.Error(), "server refused TLS connection") {
		return classifiedFailure{
			disposition: dispositionRejected, kind: sdkgo.FailureProviderRejection,
			message: "the PostgreSQL server does not accept TLS, which sslMode requires",
		}, true
	}
	var lookupFailure *net.DNSError
	if errors.As(err, &lookupFailure) && lookupFailure.IsNotFound {
		return classifiedFailure{disposition: dispositionRejected, kind: sdkgo.FailureNotFound, message: "the PostgreSQL host name does not resolve"}, true
	}
	return classifiedFailure{}, false
}

func (phase sessionPhase) description() string {
	switch phase {
	case phaseConnect:
		return "connect"
	case phaseBegin:
		return "BEGIN"
	case phaseCommit:
		return "COMMIT"
	default:
		return "the statement"
	}
}

func describeSQLState(code string) string {
	if name, isKnown := sqlStateConditionNames[code]; isKnown {
		return code + " (" + name + ")"
	}
	return code
}

func isSafeIdentifier(identifier string) bool {
	return len(identifier) <= 128 && isSafeReceiptValue(identifier)
}

func failurePointer(operationID string, failure classifiedFailure) *sdkgo.Failure {
	value := sdkgo.Failure{Kind: failure.kind, Provider: ConnectorID, Operation: operationID, Message: failure.message}
	return &value
}

func sdkFailure(operationID string, failure classifiedFailure) sdkgo.Failure {
	return *failurePointer(operationID, failure)
}

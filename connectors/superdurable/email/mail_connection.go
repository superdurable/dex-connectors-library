// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package email

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"sync"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// serverOutcome is the provider-neutral meaning of one failed IMAP or SMTP exchange.
type serverOutcome uint8

const (
	// outcomeRetryable means the server did not complete the exchange and a later attempt may succeed.
	outcomeRetryable serverOutcome = iota + 1
	// outcomeRejected means the server conclusively refused the exchange.
	outcomeRejected
	// outcomeNotFound means the mailbox or message does not exist.
	outcomeNotFound
	// outcomeInvalidResponse means the server answered with something the connector cannot parse.
	outcomeInvalidResponse
)

// serverFailure is one classified failure with its safe Failure.
type serverFailure struct {
	outcome serverOutcome
	failure sdkgo.Failure
}

// observedConn records whether the network itself failed, so a client library error can be told
// apart from a protocol error even when the library formats the network error into plain text.
type observedConn struct {
	net.Conn
	mutex           sync.Mutex
	isTransportLost bool
}

// dialMailServer opens a TCP connection and, for implicit TLS, completes the TLS handshake.
// The returned observedConn is the raw TCP connection; the net.Conn is what the protocol client uses.
func dialMailServer(ctx context.Context, endpoint mailServerEndpoint, tlsConfig *tls.Config, operation string) (*observedConn, net.Conn, *serverFailure) {
	connectCtx, cancel := context.WithTimeout(ctx, connectTimeout)
	defer cancel()
	dialer := net.Dialer{}
	rawConn, err := dialer.DialContext(connectCtx, "tcp", endpoint.address())
	if err != nil {
		return nil, nil, classifyDialError(endpoint, operation, err)
	}
	observed := &observedConn{Conn: rawConn}
	if endpoint.usesStartTLS {
		return observed, observed, nil
	}
	tlsConn := tls.Client(observed, tlsConfig)
	if err := tlsConn.HandshakeContext(connectCtx); err != nil {
		closeQuietly(tlsConn)
		return nil, nil, classifyTLSError(endpoint, operation, observed, err)
	}
	return observed, tlsConn, nil
}

// Read records a network failure before returning it.
func (conn *observedConn) Read(buffer []byte) (int, error) {
	count, err := conn.Conn.Read(buffer)
	if err != nil {
		conn.markTransportLost()
	}
	return count, err
}

// Write records a network failure before returning it.
func (conn *observedConn) Write(buffer []byte) (int, error) {
	count, err := conn.Conn.Write(buffer)
	if err != nil {
		conn.markTransportLost()
	}
	return count, err
}

// closeForDeadline marks the connection lost and closes it, so an exchange cut short by the operation's
// deadline is reported as a transport failure. A client library's own Close after an error marks nothing.
func (conn *observedConn) closeForDeadline() {
	conn.markTransportLost()
	closeQuietly(conn)
}

func (conn *observedConn) markTransportLost() {
	conn.mutex.Lock()
	conn.isTransportLost = true
	conn.mutex.Unlock()
}

func (conn *observedConn) hasLostTransport() bool {
	if conn == nil {
		return false
	}
	conn.mutex.Lock()
	defer conn.mutex.Unlock()
	return conn.isTransportLost
}

// classifyDialError treats a name that does not resolve as a rejection and every other dial failure as retryable.
func classifyDialError(endpoint mailServerEndpoint, operation string, err error) *serverFailure {
	var dnsError *net.DNSError
	if errors.As(err, &dnsError) && dnsError.IsNotFound {
		return &serverFailure{outcome: outcomeRejected, failure: emailFailure(sdkgo.FailureValidation, operation,
			fmt.Sprintf("the %s host name does not resolve; check the connection's host", endpoint.protocolLabel))}
	}
	return &serverFailure{outcome: outcomeRetryable, failure: emailFailure(sdkgo.FailureTransport, operation,
		fmt.Sprintf("the %s server could not be reached", endpoint.protocolLabel))}
}

// classifyTLSError treats certificate and handshake refusals as rejections and a lost connection as retryable.
func classifyTLSError(endpoint mailServerEndpoint, operation string, observed *observedConn, err error) *serverFailure {
	var verificationError *tls.CertificateVerificationError
	var unknownAuthorityError x509.UnknownAuthorityError
	var hostnameError x509.HostnameError
	var invalidCertificateError x509.CertificateInvalidError
	var recordHeaderError tls.RecordHeaderError
	switch {
	case errors.As(err, &verificationError), errors.As(err, &unknownAuthorityError), errors.As(err, &hostnameError),
		errors.As(err, &invalidCertificateError):
		return &serverFailure{outcome: outcomeRejected, failure: emailFailure(sdkgo.FailureProtocol, operation,
			fmt.Sprintf("the %s server certificate failed verification for the configured host", endpoint.protocolLabel))}
	case errors.As(err, &recordHeaderError):
		return &serverFailure{outcome: outcomeRejected, failure: emailFailure(sdkgo.FailureProtocol, operation,
			fmt.Sprintf("the %s server did not answer with TLS; check the connection's port and security mode", endpoint.protocolLabel))}
	case observed.hasLostTransport() || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled):
		return &serverFailure{outcome: outcomeRetryable, failure: emailFailure(sdkgo.FailureTransport, operation,
			fmt.Sprintf("the %s connection was lost during the TLS handshake", endpoint.protocolLabel))}
	default:
		return &serverFailure{outcome: outcomeRejected, failure: emailFailure(sdkgo.FailureProtocol, operation,
			fmt.Sprintf("the %s server refused the TLS handshake", endpoint.protocolLabel))}
	}
}

// isTLSHandshakeError reports a certificate or handshake failure that classifyTLSError should describe.
func isTLSHandshakeError(err error) bool {
	var verificationError *tls.CertificateVerificationError
	var unknownAuthorityError x509.UnknownAuthorityError
	var hostnameError x509.HostnameError
	var invalidCertificateError x509.CertificateInvalidError
	var recordHeaderError tls.RecordHeaderError
	var alertError tls.AlertError
	return errors.As(err, &verificationError) || errors.As(err, &unknownAuthorityError) || errors.As(err, &hostnameError) ||
		errors.As(err, &invalidCertificateError) || errors.As(err, &recordHeaderError) || errors.As(err, &alertError)
}

// closeQuietly closes a connection whose close error cannot change the operation's outcome.
func closeQuietly(conn net.Conn) {
	// The exchange already has its outcome; a failed close leaves nothing to report.
	_ = conn.Close()
}

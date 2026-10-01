// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package email

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/emersion/go-sasl"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// logoutAnswerTimeout bounds the courtesy LOGOUT so a slow server cannot delay a decided result.
const logoutAnswerTimeout = 2 * time.Second

// imapSession is one authenticated IMAP connection owned by one operation.
type imapSession struct {
	client       *imapclient.Client
	observed     *observedConn
	operation    string
	stopWatchdog func() bool
}

// connectIMAPSession connects, secures, and logs in. It never sends a credential before TLS is established,
// and ctx ending closes the connection so no IMAP call outlives the operation.
func connectIMAPSession(ctx context.Context, endpoint mailServerEndpoint, tlsConfig *tls.Config, credentials Credentials, operation string) (*imapSession, *serverFailure) {
	observed, conn, failure := dialMailServer(ctx, endpoint, tlsConfig, operation)
	if failure != nil {
		return nil, failure
	}
	session := &imapSession{observed: observed, operation: operation}
	// Closing the raw connection unblocks every pending IMAP call when the operation's deadline passes.
	session.stopWatchdog = context.AfterFunc(ctx, observed.closeForDeadline)
	options := &imapclient.Options{TLSConfig: tlsConfig}
	if endpoint.usesStartTLS {
		imapClient, err := imapclient.NewStartTLS(conn, options)
		if err != nil {
			session.stopWatchdog()
			closeQuietly(observed)
			var imapError *imap.Error
			switch {
			case errors.As(err, &imapError) && imapError.Type != imap.StatusResponseTypeBye:
				return nil, &serverFailure{outcome: outcomeRejected, failure: emailFailure(sdkgo.FailureProtocol, operation,
					"the IMAP server did not accept STARTTLS, so the connector did not log in; use implicitTLS or a server that offers STARTTLS")}
			case isTLSHandshakeError(err):
				return nil, classifyTLSError(endpoint, operation, observed, err)
			default:
				return nil, &serverFailure{outcome: outcomeRetryable, failure: emailFailure(sdkgo.FailureTransport, operation,
					"the IMAP connection was lost before STARTTLS completed")}
			}
		}
		session.client = imapClient
	} else {
		session.client = imapclient.New(conn, options)
		if err := session.client.WaitGreeting(); err != nil {
			session.close()
			return nil, session.classify("the greeting", err)
		}
	}
	if failure := session.login(credentials); failure != nil {
		session.close()
		return nil, failure
	}
	return session, nil
}

// login uses LOGIN, or AUTHENTICATE PLAIN when the server disables LOGIN; both run only over TLS.
func (session *imapSession) login(credentials Credentials) *serverFailure {
	capabilities := session.client.Caps()
	var err error
	switch {
	case !capabilities.Has(imap.CapLoginDisabled):
		err = session.client.Login(credentials.Username, credentials.Password.Reveal()).Wait()
	case slices.ContainsFunc(capabilities.AuthMechanisms(), func(mechanism string) bool { return strings.EqualFold(mechanism, sasl.Plain) }):
		err = session.client.Authenticate(sasl.NewPlainClient("", credentials.Username, credentials.Password.Reveal()))
	default:
		return &serverFailure{outcome: outcomeRejected, failure: emailFailure(sdkgo.FailureAuthentication, session.operation,
			"the IMAP server disables LOGIN and offers no AUTH=PLAIN over this TLS connection")}
	}
	if err != nil {
		return session.classify("LOGIN", err)
	}
	return nil
}

// selectMailbox selects name, read-only for queries, and checks the expected UIDVALIDITY when it is non-zero.
func (session *imapSession) selectMailbox(name string, isReadOnly bool, expectedUIDValidity uint32) (*imap.SelectData, *serverFailure) {
	selected, err := session.client.Select(name, &imap.SelectOptions{ReadOnly: isReadOnly}).Wait()
	if err != nil {
		failure := session.classify("SELECT", err)
		if failure.outcome == outcomeRejected && isMissingMailboxError(err) {
			failure.outcome, failure.failure.Kind = outcomeNotFound, sdkgo.FailureNotFound
			failure.failure.Message = "the mailbox does not exist"
		}
		return nil, failure
	}
	if selected.UIDValidity == 0 {
		return nil, &serverFailure{outcome: outcomeInvalidResponse, failure: emailFailure(sdkgo.FailureProtocol, session.operation,
			"the IMAP server selected the mailbox without reporting UIDVALIDITY")}
	}
	if expectedUIDValidity != 0 && selected.UIDValidity != expectedUIDValidity {
		return nil, &serverFailure{outcome: outcomeNotFound, failure: emailFailure(sdkgo.FailureNotFound, session.operation,
			"the mailbox's UIDVALIDITY changed, so the UID no longer identifies the message; search again")}
	}
	return selected, nil
}

// fetchMessages runs UID FETCH and returns every buffered response.
func (session *imapSession) fetchMessages(uids []imap.UID, options *imap.FetchOptions) ([]*imapclient.FetchMessageBuffer, *serverFailure) {
	messages, err := session.client.Fetch(imap.UIDSetNum(uids...), options).Collect()
	if err != nil {
		return nil, session.classify("FETCH", err)
	}
	return messages, nil
}

// close sends LOGOUT as a courtesy, waits briefly for its answer, and closes the connection.
func (session *imapSession) close() {
	if session.client != nil && !session.observed.hasLostTransport() {
		logout := session.client.Logout()
		answered := make(chan struct{})
		go func() {
			// The outcome is already decided; LOGOUT's answer cannot change it.
			_ = logout.Wait()
			close(answered)
		}()
		timer := time.NewTimer(logoutAnswerTimeout)
		select {
		case <-answered:
		case <-timer.C:
		}
		timer.Stop()
	}
	if session.client != nil {
		// Close reports only that the connection was already closed.
		_ = session.client.Close()
	}
	closeQuietly(session.observed)
	if session.stopWatchdog != nil {
		session.stopWatchdog()
	}
}

// classify maps an IMAP command failure to an outcome without repeating the server's human-readable text.
func (session *imapSession) classify(command string, err error) *serverFailure {
	operation := session.operation
	var imapError *imap.Error
	if errors.As(err, &imapError) {
		return classifyIMAPStatus(operation, command, imapError)
	}
	if session.observed.hasLostTransport() || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, context.DeadlineExceeded) {
		return &serverFailure{outcome: outcomeRetryable, failure: emailFailure(sdkgo.FailureTransport, operation,
			fmt.Sprintf("the IMAP connection was lost during %s", command))}
	}
	return &serverFailure{outcome: outcomeInvalidResponse, failure: emailFailure(sdkgo.FailureProtocol, operation,
		fmt.Sprintf("the IMAP server answered %s with a response the connector cannot parse", command))}
}

// classifyIMAPStatus maps a NO, BAD, or BYE status by its response code; the text is never copied.
func classifyIMAPStatus(operation string, command string, imapError *imap.Error) *serverFailure {
	description := fmt.Sprintf("the IMAP server answered %s with %s", command, imapError.Type)
	if imapError.Code != "" {
		description += " [" + string(imapError.Code) + "]"
	}
	if imapError.Type == imap.StatusResponseTypeBye {
		return &serverFailure{outcome: outcomeRetryable, failure: emailFailure(sdkgo.FailureAvailability, operation, description)}
	}
	if imapError.Type == imap.StatusResponseTypeBad {
		return &serverFailure{outcome: outcomeRejected, failure: emailFailure(sdkgo.FailureProtocol, operation, description)}
	}
	switch imapError.Code {
	case imap.ResponseCodeUnavailable, imap.ResponseCodeInUse, imap.ResponseCodeServerBug:
		return &serverFailure{outcome: outcomeRetryable, failure: emailFailure(sdkgo.FailureAvailability, operation, description)}
	case imap.ResponseCodeLimit:
		return &serverFailure{outcome: outcomeRetryable, failure: emailFailure(sdkgo.FailureRateLimit, operation, description)}
	case imap.ResponseCodeAuthenticationFailed, imap.ResponseCodeExpired, imap.ResponseCodePrivacyRequired, imap.ResponseCodeContactAdmin:
		return &serverFailure{outcome: outcomeRejected, failure: emailFailure(sdkgo.FailureAuthentication, operation, description)}
	case imap.ResponseCodeAuthorizationFailed, imap.ResponseCodeNoPerm:
		return &serverFailure{outcome: outcomeRejected, failure: emailFailure(sdkgo.FailureAuthorization, operation, description)}
	case imap.ResponseCodeNonExistent, imap.ResponseCodeTryCreate:
		return &serverFailure{outcome: outcomeRejected, failure: emailFailure(sdkgo.FailureNotFound, operation, description)}
	case imap.ResponseCodeOverQuota:
		return &serverFailure{outcome: outcomeRejected, failure: emailFailure(sdkgo.FailureQuotaExhausted, operation, description)}
	}
	if command == "LOGIN" {
		return &serverFailure{outcome: outcomeRejected, failure: emailFailure(sdkgo.FailureAuthentication, operation, description)}
	}
	return &serverFailure{outcome: outcomeRejected, failure: emailFailure(sdkgo.FailureProviderRejection, operation, description)}
}

// isMissingMailboxError reports a SELECT refusal that means the mailbox does not exist. Servers without
// response codes answer a plain NO, which stays a rejection.
func isMissingMailboxError(err error) bool {
	var imapError *imap.Error
	return errors.As(err, &imapError) && imapError.Type == imap.StatusResponseTypeNo &&
		(imapError.Code == imap.ResponseCodeNonExistent || imapError.Code == imap.ResponseCodeTryCreate)
}

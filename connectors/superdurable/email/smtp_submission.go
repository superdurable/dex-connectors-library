// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package email

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/emersion/go-sasl"
	"github.com/emersion/go-smtp"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// quitAnswerTimeout bounds the courtesy QUIT after the server accepted the message.
const quitAnswerTimeout = 2 * time.Second

// submissionOutcome is the result of one SMTP submission.
type submissionOutcome uint8

const (
	// submissionAccepted means the server answered the final dot with 250.
	submissionAccepted submissionOutcome = iota + 1
	// submissionNotSent means the server provably did not accept the message.
	submissionNotSent
	// submissionUncertain means the final dot may have reached the server but its answer did not arrive.
	submissionUncertain
)

// submissionResult is one classified submission; failure is set unless the message was accepted.
type submissionResult struct {
	outcome submissionOutcome
	// isRetryable reports, for submissionNotSent, a temporary refusal or a connection that failed before the final dot.
	isRetryable bool
	failure     sdkgo.Failure
}

// smtpSubmission holds one submission's connection state.
type smtpSubmission struct {
	endpoint  mailServerEndpoint
	tlsConfig *tls.Config
	observed  *observedConn
	smtp      *smtp.Client
	operation string
}

// submitToSMTPServer sends one rendered message to recipients. Every failure before the final dot is reported
// as not sent, because an SMTP server accepts a message only when it answers the final dot.
func submitToSMTPServer(ctx context.Context, endpoint mailServerEndpoint, tlsConfig *tls.Config, credentials Credentials,
	sender string, recipients []string, content []byte, operation string) submissionResult {
	observed, conn, failure := dialMailServer(ctx, endpoint, tlsConfig, operation)
	if failure != nil {
		return notSentResult(failure)
	}
	// Closing the raw connection unblocks every pending SMTP read when the operation's deadline passes.
	stopWatchdog := context.AfterFunc(ctx, observed.closeForDeadline)
	defer stopWatchdog()
	defer closeQuietly(observed)
	submission := &smtpSubmission{endpoint: endpoint, tlsConfig: tlsConfig, observed: observed, operation: operation}
	if failure := submission.open(conn); failure != nil {
		return notSentResult(failure)
	}
	if failure := submission.authenticate(credentials); failure != nil {
		return notSentResult(failure)
	}
	if size, hasSize := submission.smtp.MaxMessageSize(); hasSize && size > 0 && len(content) > size {
		return notSentResult(&serverFailure{outcome: outcomeRejected, failure: emailFailure(sdkgo.FailureProviderRejection, operation,
			fmt.Sprintf("the message is %d bytes, larger than the SMTP server's SIZE limit of %d bytes", len(content), size))})
	}
	if err := submission.smtp.Mail(sender, &smtp.MailOptions{Size: int64(len(content))}); err != nil {
		return notSentResult(submission.classify("MAIL FROM", err))
	}
	for index, recipient := range recipients {
		if err := submission.smtp.Rcpt(recipient, nil); err != nil {
			return notSentResult(submission.classify(fmt.Sprintf("RCPT TO for recipient %d of %d", index+1, len(recipients)), err))
		}
	}
	return submission.sendContent(content)
}

// open greets the server, upgrades with STARTTLS when configured, and refuses to continue without TLS.
func (submission *smtpSubmission) open(conn net.Conn) *serverFailure {
	if submission.endpoint.usesStartTLS {
		upgraded, err := smtp.NewClientStartTLS(conn, submission.tlsConfig)
		if err != nil {
			var smtpError *smtp.SMTPError
			if errors.As(err, &smtpError) || submission.observed.hasLostTransport() || isTLSHandshakeError(err) {
				return submission.classify("STARTTLS", err)
			}
			// Without a server reply or a network failure, initStartTLS fails only when STARTTLS is not offered.
			return &serverFailure{outcome: outcomeRejected, failure: emailFailure(sdkgo.FailureProtocol, submission.operation,
				"the SMTP server does not offer STARTTLS, so the connector did not authenticate; use implicitTLS or a server that offers STARTTLS")}
		}
		submission.smtp = upgraded
	} else {
		submission.smtp = smtp.NewClient(conn)
	}
	submission.smtp.CommandTimeout, submission.smtp.SubmissionTimeout = smtpCommandTimeout, smtpEndOfDataTimeout
	// Hello sends EHLO over TLS; after STARTTLS it is also where the TLS handshake happens.
	if err := submission.smtp.Hello("localhost"); err != nil {
		return submission.classify("EHLO", err)
	}
	if _, isTLS := submission.smtp.TLSConnectionState(); !isTLS {
		return &serverFailure{outcome: outcomeRejected, failure: emailFailure(sdkgo.FailureProtocol, submission.operation,
			"the SMTP connection is not encrypted, so the connector did not authenticate")}
	}
	return nil
}

// authenticate uses AUTH PLAIN, or AUTH LOGIN when the server offers only that, with the SMTP credentials.
func (submission *smtpSubmission) authenticate(credentials Credentials) *serverFailure {
	username, password := credentials.Username, credentials.Password.Reveal()
	if credentials.SMTPUsername != "" {
		username = credentials.SMTPUsername
	}
	if credentials.SMTPPassword.Reveal() != "" {
		password = credentials.SMTPPassword.Reveal()
	}
	var mechanism sasl.Client
	switch {
	case submission.smtp.SupportsAuth(sasl.Plain):
		mechanism = sasl.NewPlainClient("", username, password)
	case submission.smtp.SupportsAuth(sasl.Login):
		mechanism = sasl.NewLoginClient(username, password)
	default:
		return &serverFailure{outcome: outcomeRejected, failure: emailFailure(sdkgo.FailureAuthentication, submission.operation,
			"the SMTP server offers neither AUTH PLAIN nor AUTH LOGIN over this TLS connection")}
	}
	if err := submission.smtp.Auth(mechanism); err != nil {
		return submission.classify("AUTH", err)
	}
	return nil
}

// sendContent writes the message and classifies the answer to the final dot.
func (submission *smtpSubmission) sendContent(content []byte) submissionResult {
	dataWriter, err := submission.smtp.Data()
	if err != nil {
		return notSentResult(submission.classify("DATA", err))
	}
	// The final dot is written only by CloseWithResponse, so a failed Write never completes a submission.
	if _, err := dataWriter.Write(content); err != nil {
		return notSentResult(&serverFailure{outcome: outcomeRetryable, failure: emailFailure(sdkgo.FailureTransport, submission.operation,
			"the SMTP connection was lost before the message was complete; it was not submitted")})
	}
	if _, err := dataWriter.CloseWithResponse(); err != nil {
		var smtpError *smtp.SMTPError
		if errors.As(err, &smtpError) {
			return notSentResult(submission.classify("the end of the message", err))
		}
		return submissionResult{outcome: submissionUncertain, failure: emailFailure(sdkgo.FailureTransport, submission.operation,
			"the message was submitted but the SMTP server's answer did not arrive, so it may have been sent; it is not submitted again")}
	}
	// The server accepted the message; QUIT is a courtesy and cannot change that, so its wait is short.
	submission.smtp.CommandTimeout = quitAnswerTimeout
	_ = submission.smtp.Quit()
	return submissionResult{outcome: submissionAccepted}
}

// classify maps an SMTP failure before acceptance by its reply code; the server's text is never copied.
func (submission *smtpSubmission) classify(command string, err error) *serverFailure {
	operation := submission.operation
	var smtpError *smtp.SMTPError
	if errors.As(err, &smtpError) {
		description := fmt.Sprintf("the SMTP server answered %s with %d", command, smtpError.Code)
		if smtpError.EnhancedCode != smtp.EnhancedCodeNotSet && smtpError.EnhancedCode != smtp.NoEnhancedCode {
			description += fmt.Sprintf(" %d.%d.%d", smtpError.EnhancedCode[0], smtpError.EnhancedCode[1], smtpError.EnhancedCode[2])
		}
		switch {
		case smtpError.Code/100 == 4:
			return &serverFailure{outcome: outcomeRetryable, failure: emailFailure(sdkgo.FailureAvailability, operation, description)}
		case smtpError.Code == 530 || smtpError.Code == 534 || smtpError.Code == 535 || smtpError.Code == 538:
			return &serverFailure{outcome: outcomeRejected, failure: emailFailure(sdkgo.FailureAuthentication, operation, description)}
		case smtpError.Code == 552:
			return &serverFailure{outcome: outcomeRejected, failure: emailFailure(sdkgo.FailureResponseTooLarge, operation, description)}
		default:
			return &serverFailure{outcome: outcomeRejected, failure: emailFailure(sdkgo.FailureProviderRejection, operation, description)}
		}
	}
	if isTLSHandshakeError(err) {
		return classifyTLSError(submission.endpoint, operation, submission.observed, err)
	}
	if submission.observed.hasLostTransport() || errors.Is(err, context.DeadlineExceeded) {
		return &serverFailure{outcome: outcomeRetryable, failure: emailFailure(sdkgo.FailureTransport, operation,
			fmt.Sprintf("the SMTP connection was lost during %s", command))}
	}
	return &serverFailure{outcome: outcomeRejected, failure: emailFailure(sdkgo.FailureProtocol, operation,
		fmt.Sprintf("the SMTP server's answer to %s could not be used", command))}
}

func notSentResult(failure *serverFailure) submissionResult {
	return submissionResult{outcome: submissionNotSent, isRetryable: failure.outcome == outcomeRetryable, failure: failure.failure}
}

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package email implements generic mailbox operations as Dex connector Steps for any
// IMAP and SMTP mail provider other than Gmail and Microsoft 365.
//
// searchMessages and getMessage read one IMAP mailbox without changing it. setFlags
// and moveMessage change one message so that a repeated attempt changes nothing
// twice. sendMessage and replyToMessage submit one plain-text message over SMTP,
// at most once per Step execution, because SMTP has no idempotency key: they run
// with sync durability, record a Dex heartbeat checkpoint before submitting, and
// select uncertain instead of submitting again when an outcome is unknown.
//
// Every connection uses TLS, either implicit TLS or STARTTLS, and verifies the
// server certificate. The connector never authenticates over plaintext. It
// authenticates with a user name and an app password; OAuth 2.0 (XOAUTH2) is
// not supported in this release.
package email

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/mail"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	providerName = "email"

	// connectTimeout bounds the TCP connection, the TLS handshake, and the server greeting.
	connectTimeout = 15 * time.Second
	// imapOperationTimeout keeps every IMAP operation inside its 30-second Execute timeout.
	imapOperationTimeout = 25 * time.Second
	// submissionOperationTimeout keeps a send or reply inside its 150-second Execute timeout.
	submissionOperationTimeout = 140 * time.Second
	// smtpCommandTimeout bounds each SMTP reply before the final dot.
	smtpCommandTimeout = 30 * time.Second
	// smtpEndOfDataTimeout bounds the reply to the final dot; servers often scan mail before answering.
	smtpEndOfDataTimeout = 90 * time.Second

	maximumFromNameRunes = 128

	unicodeLineSeparator      = 0x2028
	unicodeParagraphSeparator = 0x2029
)

// Option configures Client construction.
type Option func(*clientOptions)

type clientOptions struct {
	rootCAs *x509.CertPool
}

// WithTLSRootCAs replaces the Worker's system trust store for verifying the IMAP and
// SMTP server certificates, for a server whose certificate comes from a private
// certificate authority, such as a self-hosted server or a local mail bridge. The
// connector still verifies the certificate chain and the host name. The caller keeps
// ownership of pool and must not modify it after construction.
func WithTLSRootCAs(pool *x509.CertPool) Option {
	return func(options *clientOptions) { options.rootCAs = pool }
}

// Client runs IMAP and SMTP exchanges for connector operations. It opens one new
// connection per operation and keeps no pool, so a replaced password takes effect on
// the next Step. A Client is safe for concurrent use by several Steps.
type Client struct {
	imapServer  mailServerEndpoint
	smtpServer  mailServerEndpoint
	fromAddress string
	fromName    string
	credentials sdkgo.CredentialProvider[Credentials]
	rootCAs     *x509.CertPool
}

// mailServerEndpoint is one validated server address and its TLS mode.
type mailServerEndpoint struct {
	host          string
	port          int
	usesStartTLS  bool
	protocolLabel string
}

// New validates configuration and constructs a Client. Credentials are resolved
// before every operation, so a replaced app password takes effect without a
// restart; hosts, ports, TLS modes, and the sender are startup configuration.
func New(config Config, credentials sdkgo.CredentialProvider[Credentials], options ...Option) (*Client, error) {
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	imapServer, err := newMailServerEndpoint("IMAP", config.IMAPHost, config.IMAPPort, config.IMAPSecurity == IMAPSecurityStartTLS)
	if err != nil {
		return nil, err
	}
	smtpServer, err := newMailServerEndpoint("SMTP", config.SMTPHost, config.SMTPPort, config.SMTPSecurity == SMTPSecurityStartTLS)
	if err != nil {
		return nil, err
	}
	if config.FromAddress != "" && !isBareEmailAddress(config.FromAddress) {
		return nil, errors.New("email fromAddress must be one bare ASCII address such as support@example.com")
	}
	if err := validateFromName(config.FromName); err != nil {
		return nil, err
	}
	if credentials == nil {
		return nil, errors.New("email credential provider is required")
	}
	dependencies := clientOptions{}
	for _, option := range options {
		if option == nil {
			return nil, errors.New("email connector option is nil")
		}
		option(&dependencies)
	}
	return &Client{
		imapServer: imapServer, smtpServer: smtpServer, fromAddress: config.FromAddress, fromName: config.FromName,
		credentials: credentials, rootCAs: dependencies.rootCAs,
	}, nil
}

// SearchMessages returns the searchMessages Query bound to this client.
func (client *Client) SearchMessages() SearchMessagesOperation {
	return SearchMessagesOperation{client: client}
}

// GetMessage returns the getMessage Query bound to this client.
func (client *Client) GetMessage() GetMessageOperation { return GetMessageOperation{client: client} }

// SendMessage returns the sendMessage Mutation bound to this client.
func (client *Client) SendMessage() SendMessageOperation { return SendMessageOperation{client: client} }

// ReplyToMessage returns the replyToMessage Mutation bound to this client.
func (client *Client) ReplyToMessage() ReplyToMessageOperation {
	return ReplyToMessageOperation{client: client}
}

// MoveMessage returns the moveMessage Mutation bound to this client.
func (client *Client) MoveMessage() MoveMessageOperation { return MoveMessageOperation{client: client} }

// SetFlags returns the setFlags Mutation bound to this client.
func (client *Client) SetFlags() SetFlagsOperation { return SetFlagsOperation{client: client} }

// resolveCredentials returns credentials that are safe to send in IMAP and SMTP commands, or a defect Failure.
func (client *Client) resolveCredentials(call sdkgo.Call, operation string) (Credentials, *sdkgo.Failure) {
	credentials, err := client.credentials.Resolve(call)
	if err != nil || validateResolvedCredentials(credentials) != nil {
		return Credentials{}, emailFailurePointer(sdkgo.FailureAuthentication, operation, "connection credentials are unavailable")
	}
	return credentials, nil
}

// messageSender is fromAddress, or the IMAP user name when it is a complete address, with fromName.
func (client *Client) messageSender(credentials Credentials) (messageSender, error) {
	if client.fromAddress != "" {
		return messageSender{address: client.fromAddress, name: client.fromName}, nil
	}
	if isBareEmailAddress(credentials.Username) {
		return messageSender{address: credentials.Username, name: client.fromName}, nil
	}
	return messageSender{}, errors.New("fromAddress is blank and username is not a complete address, so the sender is unknown")
}

// openIMAPSession connects to the configured IMAP server over TLS and logs in.
func (client *Client) openIMAPSession(ctx context.Context, credentials Credentials, operation string) (*imapSession, *serverFailure) {
	return connectIMAPSession(ctx, client.imapServer, client.tlsConfig(client.imapServer), credentials, operation)
}

// submitMessage submits one rendered message to the configured SMTP server over TLS.
func (client *Client) submitMessage(ctx context.Context, credentials Credentials, sender string, recipients []string, content []byte, operation string) submissionResult {
	return submitToSMTPServer(ctx, client.smtpServer, client.tlsConfig(client.smtpServer), credentials, sender, recipients, content, operation)
}

// tlsConfig verifies the certificate chain and the configured host name with TLS 1.2 or later.
func (client *Client) tlsConfig(endpoint mailServerEndpoint) *tls.Config {
	return &tls.Config{ServerName: endpoint.host, RootCAs: client.rootCAs, MinVersion: tls.VersionTLS12}
}

func (endpoint mailServerEndpoint) address() string {
	return net.JoinHostPort(endpoint.host, strconv.Itoa(endpoint.port))
}

func newMailServerEndpoint(protocolLabel string, host string, port int64, usesStartTLS bool) (mailServerEndpoint, error) {
	if !isServerHostName(host) {
		return mailServerEndpoint{}, fmt.Errorf("email %s host must be a host name or IP address without a scheme or port, such as %s.example.com",
			protocolLabel, strings.ToLower(protocolLabel))
	}
	if port < 1 || port > 65535 {
		return mailServerEndpoint{}, fmt.Errorf("email %s port must be from 1 through 65535", protocolLabel)
	}
	return mailServerEndpoint{host: host, port: int(port), usesStartTLS: usesStartTLS, protocolLabel: protocolLabel}, nil
}

// isServerHostName accepts a DNS name or an IP literal, and rejects schemes, ports, paths, and spaces.
func isServerHostName(host string) bool {
	if host == "" || len(host) > 253 || strings.TrimSpace(host) != host {
		return false
	}
	if net.ParseIP(host) != nil {
		return true
	}
	for _, label := range strings.Split(strings.TrimSuffix(host, "."), ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, character := range label {
			if !(character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' ||
				character >= '0' && character <= '9' || character == '-' || character == '_') {
				return false
			}
		}
	}
	return true
}

func validateResolvedCredentials(credentials Credentials) error {
	if err := credentials.Validate(); err != nil {
		return err
	}
	for _, value := range []string{credentials.Username, credentials.Password.Reveal(), credentials.SMTPUsername, credentials.SMTPPassword.Reveal()} {
		if !utf8.ValidString(value) || strings.ContainsFunc(value, isControlCharacter) {
			return errors.New("email credentials cannot contain control characters")
		}
	}
	return nil
}

func validateFromName(name string) error {
	if name == "" {
		return nil
	}
	if strings.TrimSpace(name) != name || !utf8.ValidString(name) || strings.ContainsFunc(name, isControlCharacter) ||
		utf8.RuneCountInString(name) > maximumFromNameRunes {
		return fmt.Errorf("email fromName must be at most %d characters without surrounding spaces or line breaks", maximumFromNameRunes)
	}
	return nil
}

// isBareEmailAddress accepts one ASCII addr-spec such as jane@example.com, without a display name or angle brackets.
func isBareEmailAddress(value string) bool {
	if value == "" || len(value) > 254 || strings.ContainsFunc(value, func(character rune) bool {
		return character > unicode.MaxASCII || unicode.IsSpace(character) || isControlCharacter(character)
	}) {
		return false
	}
	address, err := mail.ParseAddress(value)
	if err != nil || address.Name != "" || address.Address != value {
		return false
	}
	at := strings.LastIndexByte(value, '@')
	return at > 0 && isServerHostName(value[at+1:]) && strings.Contains(value[at+1:], ".")
}

// isControlCharacter also rejects the Unicode line and paragraph separators, which some readers treat as line breaks.
func isControlCharacter(character rune) bool {
	return unicode.IsControl(character) || character == unicodeLineSeparator || character == unicodeParagraphSeparator
}

func emailFailure(kind sdkgo.FailureKind, operation string, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: providerName, Operation: operation, Message: message}
}

func emailFailurePointer(kind sdkgo.FailureKind, operation string, message string) *sdkgo.Failure {
	failure := emailFailure(kind, operation, message)
	return &failure
}

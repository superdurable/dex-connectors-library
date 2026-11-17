// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package webfeed reads public web feeds, sitemaps, HTML page metadata, and readable page text through
// typed Dex Query Steps.
//
// The connector needs no credentials. Every request is a GET to a public address over HTTPS: an http URL
// is upgraded to https before connecting, the client connects only when every address a host resolves to
// is public (providerhttp.PublicHTTPS), and redirects are followed manually, at most five hops, through the
// same checks. Requests carry no cookies and no credentials, and responses are read within the connection's
// byte limits.
//
// Applications open the connection with the generated NewProjectConnection and use the generated Step
// factories NewReadFeedStep, NewDiscoverFeedsStep, NewReadSitemapStep, NewReadPageMetadataStep, and
// NewReadPageStep. The runnable example in examples/blog-post-check wires all five into one Flow.
package webfeed

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// providerID names the provider in Failures and connection references.
const providerID = "web-feed"

// maxUserAgentBytes bounds the configured User-Agent header.
const maxUserAgentBytes = 256

// Option supplies a non-serializable client dependency to New or NewProjectConnection.
type Option interface{ applyClientOption(*clientOptions) }

// clientOptions holds the dependencies that options replace before New builds the HTTP client.
type clientOptions struct {
	httpClient      *http.Client
	clock           func() time.Time
	newPolicyClient func(caller *http.Client, requestTimeout time.Duration) (*http.Client, error)
}

type httpClientOption struct{ client *http.Client }

func (option httpClientOption) applyClientOption(options *clientOptions) {
	options.httpClient = option.client
}

// WithHTTPClient supplies the base client whose TLS and connection-pool settings requests use, such as a
// client that trusts an organization's root certificates. Its Transport must be nil or an *http.Transport.
// New copies the client and leaves the original unchanged; the copy keeps a nonzero Timeout, applies the
// public HTTPS address policy, ignores proxy settings, follows no redirect automatically, and has no
// cookie jar.
func WithHTTPClient(client *http.Client) Option { return httpClientOption{client: client} }

// Client holds immutable request settings for one connection and is safe for concurrent use by
// several Steps. New never contacts the network.
type Client struct {
	httpClient       *http.Client
	userAgent        string
	maxDocumentBytes int64
	maxPageBytes     int64
	clock            func() time.Time
}

// ReadFeedOperation reads one feed document per invocation, following redirects.
type ReadFeedOperation struct{ client *Client }

// DiscoverFeedsOperation reads one HTML page per invocation and lists the feeds it advertises.
type DiscoverFeedsOperation struct{ client *Client }

// ReadSitemapOperation reads one sitemap or sitemap index per invocation, following redirects.
type ReadSitemapOperation struct{ client *Client }

// ReadPageMetadataOperation reads the head metadata of one HTML page per invocation.
type ReadPageMetadataOperation struct{ client *Client }

// ReadPageOperation reads the readable text of one HTML or plain-text page per invocation.
type ReadPageOperation struct{ client *Client }

// New validates config and builds a Client. Zero fields take their manifest defaults. The connector has
// no credentials, so the credential source is ignored and may be nil.
//
// New fails when a setting is out of range or when an option supplies a client whose Transport is not an
// *http.Transport, because the address policy must own the dialer.
func New(config Config, _ CredentialSource, options ...Option) (*Client, error) {
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if err := validateLimits(config); err != nil {
		return nil, err
	}
	dependencies := clientOptions{clock: time.Now, newPolicyClient: newPublicHTTPSClient}
	for _, option := range options {
		if option == nil {
			return nil, errors.New("web feed connector option is nil")
		}
		option.applyClientOption(&dependencies)
	}
	if dependencies.clock == nil {
		return nil, errors.New("web feed connector clock is required")
	}
	httpClient, err := dependencies.newPolicyClient(dependencies.httpClient, config.Timeout)
	if err != nil {
		return nil, fmt.Errorf("web feed HTTP client: %w", err)
	}
	// The policy client is a private copy, so dropping its jar cannot affect the caller's client.
	httpClient.Jar = nil
	return &Client{
		httpClient: httpClient, userAgent: config.UserAgent,
		maxDocumentBytes: config.MaxDocumentBytes, maxPageBytes: config.MaxPageBytes, clock: dependencies.clock,
	}, nil
}

// ReadFeed returns the readFeed Query bound to this client.
func (client *Client) ReadFeed() ReadFeedOperation { return ReadFeedOperation{client: client} }

// DiscoverFeeds returns the discoverFeeds Query bound to this client.
func (client *Client) DiscoverFeeds() DiscoverFeedsOperation {
	return DiscoverFeedsOperation{client: client}
}

// ReadSitemap returns the readSitemap Query bound to this client.
func (client *Client) ReadSitemap() ReadSitemapOperation { return ReadSitemapOperation{client: client} }

// ReadPageMetadata returns the readPageMetadata Query bound to this client.
func (client *Client) ReadPageMetadata() ReadPageMetadataOperation {
	return ReadPageMetadataOperation{client: client}
}

// ReadPage returns the readPage Query bound to this client.
func (client *Client) ReadPage() ReadPageOperation { return ReadPageOperation{client: client} }

func validateLimits(config Config) error {
	switch {
	case config.Timeout < time.Second:
		return errors.New("configuration timeout must be at least one second")
	case config.MaxDocumentBytes < 1024 || config.MaxDocumentBytes > 256<<20:
		return errors.New("configuration maxDocumentBytes must be from 1024 through 268435456")
	case config.MaxPageBytes < 1024 || config.MaxPageBytes > 64<<20:
		return errors.New("configuration maxPageBytes must be from 1024 through 67108864")
	case !isPrintableHeaderText(config.UserAgent, maxUserAgentBytes):
		return errors.New("configuration userAgent must be 1 to 256 printable ASCII characters")
	}
	return nil
}

// isPrintableHeaderText reports whether value is nonempty printable ASCII, spaces included, of at most
// maxBytes bytes, so it can travel in one header unchanged.
func isPrintableHeaderText(value string, maxBytes int) bool {
	if value == "" || len(value) > maxBytes {
		return false
	}
	for index := 0; index < len(value); index++ {
		if value[index] < 0x20 || value[index] > 0x7E {
			return false
		}
	}
	return true
}

// newPublicHTTPSClient connects only over https and only when every resolved address is public.
func newPublicHTTPSClient(caller *http.Client, requestTimeout time.Duration) (*http.Client, error) {
	return providerhttp.NewAddressPolicyClient(caller, requestTimeout, providerhttp.PublicHTTPS)
}

func newFailure(operationID string, kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: providerID, Operation: operationID, Message: message}
}

func failedBranch[T any](operationID string, branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[T] {
	var value T
	failure := newFailure(operationID, kind, message)
	return sdkgo.NewQueryBranch(branch, value, &failure, sdkgo.Receipt{})
}

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package sns exposes Amazon SNS topics and publishing to Dex Flows.
package sns

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

var regionPattern = regexp.MustCompile(`^[a-z]{2}(?:-gov)?-[a-z]+-[0-9]+$`)

// Option configures the provider HTTP dependency.
type Option interface{ applyClientOption(*Client) }
type httpClientOption struct{ client *http.Client }

func (option httpClientOption) applyClientOption(client *Client) { client.httpClient = option.client }

// WithHTTPClient supplies a transport for production or provider tests.
func WithHTTPClient(httpClient *http.Client) Option { return httpClientOption{httpClient} }

type signingTimeOption struct{ now func() time.Time }

func (option signingTimeOption) applyClientOption(client *Client) { client.now = option.now }

// Client holds the regional endpoint and resolves AWS credentials per call.
type Client struct {
	endpoint                string
	region                  string
	maxResponseBytes        int64
	credentials             CredentialSource
	httpClient              *http.Client
	signer                  *v4.Signer
	now                     func() time.Time
	certificateRoots        *x509.CertPool
	notificationEndpointsMu sync.Mutex
	notificationEndpoints   map[sdkgo.ConnectionRef]*snsNotificationEndpoint
}

// New validates the endpoint, Region, and dependencies without contacting AWS.
func New(config Config, credentials CredentialSource, options ...Option) (*Client, error) {
	if credentials == nil {
		return nil, errors.New("SNS credential source is required")
	}
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if !regionPattern.MatchString(config.Region) {
		return nil, errors.New("SNS Region is invalid")
	}
	endpoint := config.Endpoint
	if endpoint == "" {
		endpoint = "https://sns." + config.Region + ".amazonaws.com"
	}
	validated, err := providerhttp.ValidateBaseURL(endpoint)
	if err != nil {
		return nil, fmt.Errorf("SNS endpoint: %w", err)
	}
	certificateRoots, err := x509.SystemCertPool()
	if err != nil {
		return nil, fmt.Errorf("load system certificate roots: %w", err)
	}
	client := &Client{endpoint: strings.TrimRight(validated, "/"), region: config.Region, maxResponseBytes: config.MaxResponseBytes, credentials: credentials, signer: v4.NewSigner(), now: time.Now, certificateRoots: certificateRoots, notificationEndpoints: map[sdkgo.ConnectionRef]*snsNotificationEndpoint{}}
	for _, option := range options {
		if option == nil {
			return nil, errors.New("SNS client option is required")
		}
		option.applyClientOption(client)
	}
	client.httpClient = providerhttp.NewProviderHTTPClient(client.httpClient, config.Timeout)
	return client, nil
}

var errCredential = errors.New("SNS credential unavailable")
var errRequest = errors.New("SNS request failed")

type providerResponse struct {
	body       []byte
	status     int
	retryAfter time.Duration
	err        error
}

func (client *Client) listTopics(call sdkgo.Call, nextToken string) providerResponse {
	query := url.Values{"Action": {"ListTopics"}, "Version": {"2010-03-31"}}
	if nextToken != "" {
		query.Set("NextToken", nextToken)
	}
	return client.request(call, query)
}

func (client *Client) request(call sdkgo.Call, query url.Values) providerResponse {
	return client.requestWithContext(call, call.Context, query)
}

func (client *Client) requestWithContext(call sdkgo.Call, requestContext context.Context, query url.Values) providerResponse {
	credentials, err := client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil {
		return providerResponse{err: errCredential}
	}
	accessKey := credentials.AccessKeyID.Reveal()
	secretKey := credentials.SecretAccessKey.Reveal()
	sessionToken := credentials.SessionToken.Reveal()
	if !providerhttp.IsHeaderSafeCredential(accessKey) || !providerhttp.IsHeaderSafeCredential(secretKey) || (sessionToken != "" && !providerhttp.IsHeaderSafeCredential(sessionToken)) {
		return providerResponse{err: errCredential}
	}
	method := http.MethodGet
	requestURL := client.endpoint + "/?" + query.Encode()
	var body []byte
	if query.Get("Action") == "Publish" {
		method = http.MethodPost
		requestURL = client.endpoint + "/"
		body = []byte(query.Encode())
	}
	request, err := http.NewRequestWithContext(requestContext, method, requestURL, bytes.NewReader(body))
	if err != nil {
		return providerResponse{err: errRequest}
	}
	if method == http.MethodPost {
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	digest := sha256.Sum256(body)
	signingCredentials := aws.Credentials{AccessKeyID: accessKey, SecretAccessKey: secretKey, SessionToken: sessionToken}
	if err := client.signer.SignHTTP(request.Context(), signingCredentials, request, hex.EncodeToString(digest[:]), "sns", client.region, client.now().UTC()); err != nil {
		return providerResponse{err: errRequest}
	}
	response, err := client.httpClient.Do(request)
	if err != nil {
		return providerResponse{err: errRequest}
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), client.now())
	responseBody, err := providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes)
	for _, secret := range []string{accessKey, secretKey, sessionToken} {
		if secret != "" && bytes.Contains(responseBody, []byte(secret)) {
			return providerResponse{status: response.StatusCode, retryAfter: retryAfter, err: errCredential}
		}
	}
	return providerResponse{body: responseBody, status: response.StatusCode, retryAfter: retryAfter, err: err}
}

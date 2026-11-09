// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package redshift exposes Amazon Redshift statement results to Dex Flows.
package redshift

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"

	"regexp"
	"strings"
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
	endpoint         string
	region           string
	maxResponseBytes int64
	credentials      CredentialSource
	httpClient       *http.Client
	signer           *v4.Signer
	now              func() time.Time
}

// New validates the endpoint, Region, and dependencies without contacting AWS.
func New(config Config, credentials CredentialSource, options ...Option) (*Client, error) {
	if credentials == nil {
		return nil, errors.New("Redshift credential source is required")
	}
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if !regionPattern.MatchString(config.Region) {
		return nil, errors.New("Redshift Region is invalid")
	}
	endpoint := config.Endpoint
	if endpoint == "" {
		endpoint = "https://redshift-data." + config.Region + ".amazonaws.com"
	}
	validated, err := providerhttp.ValidateBaseURL(endpoint)
	if err != nil {
		return nil, fmt.Errorf("Redshift endpoint: %w", err)
	}
	client := &Client{endpoint: strings.TrimRight(validated, "/"), region: config.Region, maxResponseBytes: config.MaxResponseBytes, credentials: credentials, signer: v4.NewSigner(), now: time.Now}
	for _, option := range options {
		if option == nil {
			return nil, errors.New("Redshift client option is required")
		}
		option.applyClientOption(client)
	}
	client.httpClient = providerhttp.NewProviderHTTPClient(client.httpClient, config.Timeout)
	return client, nil
}

var errCredential = errors.New("Redshift credential unavailable")
var errRequest = errors.New("Redshift request failed")

type providerResponse struct {
	body       []byte
	status     int
	retryAfter time.Duration
	err        error
}

func (client *Client) getStatementResult(call sdkgo.Call, payload []byte) providerResponse {
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
	request, err := http.NewRequestWithContext(call.Context, http.MethodPost, client.endpoint+"/", bytes.NewReader(payload))
	if err != nil {
		return providerResponse{err: errRequest}
	}
	request.Header.Set("Content-Type", "application/x-amz-json-1.1")
	request.Header.Set("X-Amz-Target", "RedshiftData.GetStatementResult")
	digest := sha256.Sum256(payload)
	signingCredentials := aws.Credentials{AccessKeyID: accessKey, SecretAccessKey: secretKey, SessionToken: sessionToken}
	if err := client.signer.SignHTTP(request.Context(), signingCredentials, request, hex.EncodeToString(digest[:]), "redshift-data", client.region, client.now().UTC()); err != nil {
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

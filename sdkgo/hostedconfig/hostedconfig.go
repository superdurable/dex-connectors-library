// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package hostedconfig resolves Connector credentials through a trusted Superverse broker.
package hostedconfig

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	// BrokerURLEnvironmentVariable names the trusted credential broker base URL.
	BrokerURLEnvironmentVariable = "SUPERVERSE_CONNECTOR_BROKER_URL"
	// WorkloadTokenFileEnvironmentVariable names the projected workload-token file.
	WorkloadTokenFileEnvironmentVariable = "SUPERVERSE_CONNECTOR_WORKLOAD_TOKEN_FILE"
	defaultMaximumResponseBytes          = int64(1 << 20)
)

var workloadTokenPattern = regexp.MustCompile(`^[A-Za-z0-9._~-]+$`)

// CredentialDecoder converts the broker's secret JSON into a Connector credential type.
// Implementations must validate the complete decoded value before returning it.
type CredentialDecoder[C any] func(json.RawMessage) (C, error)

// Config binds one provider to its broker, Connector, and logical connection.
type Config struct {
	// BrokerURL is the trusted absolute HTTP(S) base URL. Paths are allowed; credentials, queries, and fragments are not.
	BrokerURL string
	// WorkloadTokenFile is an absolute regular file readable only by its owner. The provider reloads it for every call.
	WorkloadTokenFile string
	// ConnectorID is the manifest Connector identity authorized for this provider.
	ConnectorID string
	// ConnectionName is the logical connection authorized for this provider.
	ConnectionName string
	// HTTPClient overrides the broker client. Nil uses a redirect-rejecting default client.
	HTTPClient *http.Client
	// MaximumResponseBytes limits the complete broker response. Zero uses one mebibyte.
	MaximumResponseBytes int64
}

// CredentialProvider resolves operation-scoped credentials without exposing tenancy selectors.
// It is intentionally not a RefreshingCredentialProvider because the trusted broker owns refresh and persistence.
type CredentialProvider[C any] struct {
	resolveURL           string
	workloadTokenFile    string
	connectorID          string
	connectionName       string
	httpClient           *http.Client
	maximumResponseBytes int64
	decoder              CredentialDecoder[C]
}

type resolveRequest struct {
	ConnectorID    string       `json:"connectorId"`
	ConnectionName string       `json:"connectionName"`
	OperationID    string       `json:"operationId"`
	CallID         sdkgo.CallID `json:"callId"`
}

type resolveResponse struct {
	Credentials json.RawMessage `json:"credentials"`
}

type errorResponse struct {
	Code string `json:"code"`
}

// NewCredentialProvider validates cfg and returns a broker-backed credential provider.
func NewCredentialProvider[C any](cfg *Config, decoder CredentialDecoder[C]) (*CredentialProvider[C], error) {
	if cfg == nil {
		return nil, fmt.Errorf("hosted Connector credential configuration is required")
	}
	if decoder == nil {
		return nil, fmt.Errorf("hosted Connector credential decoder is required")
	}
	resolveURL, err := buildResolveURL(cfg.BrokerURL)
	if err != nil {
		return nil, err
	}
	workloadTokenFile, err := validateWorkloadTokenFilePath(cfg.WorkloadTokenFile)
	if err != nil {
		return nil, err
	}
	if err := (sdkgo.OperationRef{ConnectorID: cfg.ConnectorID, OperationID: "validateConnection"}).Validate(); err != nil {
		return nil, fmt.Errorf("hosted Connector ID is invalid: %w", err)
	}
	connection := sdkgo.ConnectionRef{Provider: "hosted", Name: cfg.ConnectionName}
	if err := connection.Validate(); err != nil {
		return nil, fmt.Errorf("hosted Connector connection is invalid: %w", err)
	}
	maximumResponseBytes := cfg.MaximumResponseBytes
	if maximumResponseBytes == 0 {
		maximumResponseBytes = defaultMaximumResponseBytes
	}
	if maximumResponseBytes < 1 {
		return nil, fmt.Errorf("hosted Connector maximum response size must be positive")
	}
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
			return errors.New("hosted Connector credential broker redirects are not allowed")
		}}
	}
	return &CredentialProvider[C]{
		resolveURL: resolveURL, workloadTokenFile: workloadTokenFile,
		connectorID: cfg.ConnectorID, connectionName: cfg.ConnectionName,
		httpClient: httpClient, maximumResponseBytes: maximumResponseBytes, decoder: decoder,
	}, nil
}

// NewCredentialProviderFromEnvironment loads the broker URL and workload-token path from their environment variables.
func NewCredentialProviderFromEnvironment[C any](
	connectorID string,
	connectionName string,
	decoder CredentialDecoder[C],
) (*CredentialProvider[C], error) {
	return NewCredentialProvider(&Config{
		BrokerURL: os.Getenv(BrokerURLEnvironmentVariable), WorkloadTokenFile: os.Getenv(WorkloadTokenFileEnvironmentVariable),
		ConnectorID: connectorID, ConnectionName: connectionName,
	}, decoder)
}

// Resolve obtains the minimum credential for call's operation from the trusted broker.
func (provider *CredentialProvider[C]) Resolve(call sdkgo.Call) (C, error) {
	var zero C
	if provider == nil {
		return zero, fmt.Errorf("hosted Connector credential provider is required")
	}
	if call.Context == nil {
		return zero, fmt.Errorf("hosted Connector call context is required")
	}
	if err := call.ID.Validate(); err != nil {
		return zero, err
	}
	if err := call.Operation.Validate(); err != nil {
		return zero, err
	}
	if call.Operation.ConnectorID != provider.connectorID {
		return zero, fmt.Errorf("hosted Connector operation does not match the configured Connector")
	}
	if call.Connection.Name != provider.connectionName {
		return zero, fmt.Errorf("hosted Connector call does not match the configured connection")
	}
	workloadToken, err := readWorkloadToken(provider.workloadTokenFile)
	if err != nil {
		return zero, err
	}
	contents, err := json.Marshal(resolveRequest{
		ConnectorID: provider.connectorID, ConnectionName: provider.connectionName,
		OperationID: call.Operation.OperationID, CallID: call.ID,
	})
	if err != nil {
		return zero, fmt.Errorf("encode hosted Connector credential request: %w", err)
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodPost, provider.resolveURL, bytes.NewReader(contents))
	if err != nil {
		return zero, fmt.Errorf("create hosted Connector credential request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+workloadToken)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	response, err := provider.httpClient.Do(request)
	if err != nil {
		return zero, fmt.Errorf("call hosted Connector credential broker: %w", err)
	}
	defer func() {
		// The complete response body is consumed below.
		_ = response.Body.Close()
	}()
	responseContents, err := io.ReadAll(io.LimitReader(response.Body, provider.maximumResponseBytes+1))
	if err != nil {
		return zero, fmt.Errorf("read hosted Connector credential response: %w", err)
	}
	if int64(len(responseContents)) > provider.maximumResponseBytes {
		return zero, fmt.Errorf("hosted Connector credential response exceeds the configured limit")
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		if isReauthorizationRequired(responseContents) {
			return zero, sdkgo.NewReauthorizationRequiredError(nil)
		}
		return zero, fmt.Errorf("hosted Connector credential broker returned HTTP %d", response.StatusCode)
	}
	var resolved resolveResponse
	if err := decodeStrict(responseContents, &resolved); err != nil {
		return zero, fmt.Errorf("decode hosted Connector credential response: %w", err)
	}
	if len(resolved.Credentials) == 0 || bytes.Equal(resolved.Credentials, []byte("null")) {
		return zero, fmt.Errorf("hosted Connector credential broker returned no credential")
	}
	credentials, err := provider.decoder(resolved.Credentials)
	if err != nil {
		return zero, fmt.Errorf("decode hosted Connector credential: %w", err)
	}
	return credentials, nil
}

func buildResolveURL(value string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || parsed.Hostname() == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return "", fmt.Errorf("hosted Connector credential broker URL must be absolute HTTP(S)")
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", fmt.Errorf("hosted Connector credential broker URL must not contain credentials, query, or fragment")
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + "/v1/credentials/resolve"
	return parsed.String(), nil
}

func validateWorkloadTokenFilePath(value string) (string, error) {
	if strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("hosted Connector workload token file is required")
	}
	absolutePath, err := filepath.Abs(value)
	if err != nil {
		return "", fmt.Errorf("resolve hosted Connector workload token file: %w", err)
	}
	return absolutePath, nil
}

func readWorkloadToken(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("inspect hosted Connector workload token file: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
		return "", fmt.Errorf("hosted Connector workload token file must be a private regular file")
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read hosted Connector workload token file: %w", err)
	}
	token := strings.TrimSpace(string(contents))
	if len(token) < 16 || len(token) > 4096 || !workloadTokenPattern.MatchString(token) {
		return "", fmt.Errorf("hosted Connector workload token is invalid")
	}
	return token, nil
}

func isReauthorizationRequired(contents []byte) bool {
	var response errorResponse
	return json.Unmarshal(contents, &response) == nil && response.Code == "reauthorization_required"
}

func decodeStrict(contents []byte, destination any) error {
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return fmt.Errorf("response must contain one JSON document")
	}
	return nil
}

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package creatio

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

type odataResponse struct {
	status         int
	body           []byte
	retryAfter     time.Duration
	err            error
	isBodyTooLarge bool
}

var errInvalidAccessToken = errors.New("Creatio access token unavailable")

func (client *Client) accessToken(call sdkgo.Call) (string, error) {
	credentials, err := sdkgo.ResolveCredential(call.Context, client.credentials, call, client.refreshDriver)
	if err != nil {
		return "", err
	}
	token := credentials.AccessToken.Reveal()
	if !providerhttp.IsHeaderSafeCredential(token) {
		return "", errInvalidAccessToken
	}
	return token, nil
}

func (client *Client) doODataRequest(requestContext context.Context, token string, method string, path string, body []byte) odataResponse {
	var bodyReader io.Reader
	if body != nil {
		bodyReader = bytes.NewReader(body)
	}
	request, err := http.NewRequestWithContext(requestContext, method, client.baseURL+path, bodyReader)
	if err != nil {
		return odataResponse{err: err}
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := client.httpClient.Do(request)
	if err != nil {
		return odataResponse{err: err}
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	responseBody, readErr := providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes)
	if bytes.Contains(responseBody, []byte(token)) {
		return odataResponse{status: response.StatusCode, err: errors.New("Creatio response included a credential")}
	}
	return odataResponse{status: response.StatusCode, body: responseBody, retryAfter: retryAfter, err: readErr, isBodyTooLarge: errors.Is(readErr, providerhttp.ErrBodyTooLarge)}
}

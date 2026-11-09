// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package box

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"sync/atomic"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

func (client *Client) upload(call sdkgo.Call, fileID string, name string, content []byte) fileResponse {
	accessToken, err := client.resolveAccessToken(call)
	if err != nil {
		return fileResponse{err: err}
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	attributes, err := json.Marshal(struct {
		Name string `json:"name"`
	}{Name: name})
	if err != nil {
		return fileResponse{err: errRequest}
	}
	if err := writer.WriteField("attributes", string(attributes)); err != nil {
		return fileResponse{err: errRequest}
	}
	filePart, err := writer.CreateFormFile("file", name)
	if err != nil {
		return fileResponse{err: errRequest}
	}
	if _, err := filePart.Write(content); err != nil {
		return fileResponse{err: errRequest}
	}
	if err := writer.Close(); err != nil {
		return fileResponse{err: errRequest}
	}
	var wasRequestSent atomic.Bool
	trace := &httptrace.ClientTrace{WroteRequest: func(info httptrace.WroteRequestInfo) {
		if info.Err == nil {
			wasRequestSent.Store(true)
		}
	}}
	request, err := http.NewRequestWithContext(httptrace.WithClientTrace(call.Context, trace), http.MethodPost, client.uploadBaseURL+"/2.0/files/"+url.PathEscape(fileID)+"/content", &body)
	if err != nil {
		return fileResponse{err: errRequest}
	}
	request.Header.Set("Authorization", "Bearer "+accessToken)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "Dex Connector (https://superdurable.io)")
	response, err := client.httpClient.Do(request)
	if err != nil {
		return fileResponse{err: errRequest, requestSent: wasRequestSent.Load()}
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	responseBody, err := providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes)
	if bytes.Contains(responseBody, []byte(accessToken)) {
		return fileResponse{status: response.StatusCode, retryAfter: retryAfter, requestSent: true, err: errCredential}
	}
	return fileResponse{body: responseBody, status: response.StatusCode, retryAfter: retryAfter, requestSent: true, err: err}
}

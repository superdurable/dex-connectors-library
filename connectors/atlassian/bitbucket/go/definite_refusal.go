// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package bitbucket

import (
	"bytes"
	"encoding/json"
	"net/http"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

func mutationAuthorizationAttempt[T any](call sdkgo.Call, operation string, authorization authResult) sdkgo.MutationAttempt[T] {
	if call.Context.Attempt() > 1 {
		return sdkgo.NewMutationRetry[T](*failureFor(operation, sdkgo.FailureTransport, "an earlier Bitbucket write remains unconfirmed"), 0)
	}
	return sdkgo.NewMutationBranch(authorization.branch, *new(T), authorization.failure, sdkgo.Receipt{})
}

func isDefiniteRefusalResponse(response *http.Response, maxResponseBytes int64, attempt ...int32) bool {
	if len(attempt) > 0 && attempt[0] > 1 {
		return false
	}
	if !isDefiniteRefusalStatus(response.StatusCode) {
		return false
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, maxResponseBytes)
	if err != nil {
		return false
	}
	var rejection struct {
		Type   string          `json:"type"`
		Data   json.RawMessage `json:"data"`
		Result json.RawMessage `json:"result"`
		Error  struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	return json.Unmarshal(body, &rejection) == nil && rejection.Type == "error" && rejection.Error.Message != "" &&
		(len(rejection.Data) == 0 || bytes.Equal(bytes.TrimSpace(rejection.Data), []byte("null"))) &&
		(len(rejection.Result) == 0 || bytes.Equal(bytes.TrimSpace(rejection.Result), []byte("null")))
}

// isDefiniteRefusalStatus identifies HTTP statuses that prove a provider refused a write.
func isDefiniteRefusalStatus(status int) bool {
	switch status {
	case http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound,
		http.StatusLengthRequired, http.StatusRequestEntityTooLarge, http.StatusRequestURITooLong,
		http.StatusUnsupportedMediaType, http.StatusUnprocessableEntity:
		return true
	}
	return false
}

func hasBitbucketContradictoryData(body []byte) bool {
	var envelope map[string]json.RawMessage
	if json.Unmarshal(body, &envelope) != nil {
		return false
	}
	if bytes.Equal(bytes.TrimSpace(envelope["success"]), []byte("false")) || bytes.Equal(bytes.TrimSpace(envelope["type"]), []byte(`"error"`)) {
		return true
	}
	for _, field := range []string{"error", "errors"} {
		value := bytes.TrimSpace(envelope[field])
		if len(value) > 0 && !bytes.Equal(value, []byte("null")) && !bytes.Equal(value, []byte("[]")) && !bytes.Equal(value, []byte("{}")) {
			return true
		}
	}
	return false
}

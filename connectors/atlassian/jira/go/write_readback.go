// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package jira

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const jiraCallPropertyKey = "dex.connector.call"

type jiraCallProperty struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type jiraCallPropertyResponse struct {
	Value string `json:"value"`
}

type writeReadbackStatus uint8

const (
	writeReadbackMissing writeReadbackStatus = iota + 1
	writeReadbackFound
	writeReadbackRetry
)

type issueReadbackPage struct {
	Issues []issueReadbackCandidate `json:"issues"`
}

type issueReadbackCandidate struct {
	ID         string                     `json:"id"`
	Key        string                     `json:"key"`
	Properties map[string]json.RawMessage `json:"properties"`
	Fields     struct {
		Summary string `json:"summary"`
	} `json:"fields"`
}

type commentReadbackPage struct {
	Comments []commentResource `json:"comments"`
}

func jiraCallPropertyForCall(call sdkgo.Call) jiraCallProperty {
	digest := sha256.Sum256([]byte(call.ID))
	return jiraCallProperty{Key: jiraCallPropertyKey, Value: hex.EncodeToString(digest[:])}
}

func readbackExchangeStatus(result exchangeResult) writeReadbackStatus {
	if result.err != nil || result.response.hasBodyReadFailed || isRetryableReadStatus(result.response.statusCode) {
		return writeReadbackRetry
	}
	if result.response.statusCode != http.StatusOK || result.response.isBodyTooLarge || result.response.reflectsSecret {
		return writeReadbackMissing
	}
	return writeReadbackFound
}

func matchesJiraCallProperty(raw json.RawMessage, expected string) bool {
	var propertyValue string
	if json.Unmarshal(raw, &propertyValue) == nil {
		return strings.EqualFold(propertyValue, expected)
	}
	var property jiraCallPropertyResponse
	return json.Unmarshal(raw, &property) == nil && strings.EqualFold(property.Value, expected)
}

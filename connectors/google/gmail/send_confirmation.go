// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package gmail

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const maxSentMessageMatches = 10

type sentMessageSearchResponse struct {
	Messages      []sendResponse `json:"messages"`
	NextPageToken string         `json:"nextPageToken"`
}

func (client *Client) findSentMessageByCallID(
	ctx context.Context, call sdkgo.Call, credentials Credentials,
) (sendResponse, error) {
	messageID := "<" + string(call.IdempotencyKey) + "@dex.superdurable.dev>"
	query := url.Values{"q": {"in:sent rfc822msgid:" + messageID}, "maxResults": {"10"}}
	target := strings.TrimRight(client.endpoint.String(), "/") + "/users/me/messages?" + query.Encode()
	response, _, err := client.doAuthenticatedRequest(ctx, call, credentials, http.MethodGet, target, nil, "")
	if err != nil {
		return sendResponse{}, fmt.Errorf("Gmail sent-message lookup failed: %w", err)
	}
	defer response.Body.Close()
	content, err := io.ReadAll(io.LimitReader(response.Body, client.maxResponseBytes+1))
	if err != nil || int64(len(content)) > client.maxResponseBytes || response.StatusCode != http.StatusOK {
		return sendResponse{}, fmt.Errorf("Gmail sent-message lookup did not return a usable response")
	}
	var matches sentMessageSearchResponse
	if err := json.Unmarshal(content, &matches); err != nil || len(matches.Messages) > maxSentMessageMatches || matches.NextPageToken != "" {
		return sendResponse{}, fmt.Errorf("Gmail sent-message lookup returned an invalid response")
	}
	for _, message := range matches.Messages {
		if message.ID == "" || message.ThreadID == "" {
			return sendResponse{}, fmt.Errorf("Gmail sent-message lookup returned an incomplete message")
		}
	}
	if len(matches.Messages) == 0 {
		return sendResponse{}, nil
	}
	return matches.Messages[0], nil
}

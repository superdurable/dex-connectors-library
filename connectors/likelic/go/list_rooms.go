// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package likelic

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// ListRoomsInput selects a one-based page of at most twenty rooms.
type ListRoomsInput struct {
	// Page is a positive page number, starting at one.
	Page int `json:"page"`
}

// Room is one notification target with no group name, company, or member details.
type Room struct {
	// Channel is wework or qq.
	Channel string `json:"channel"`
	// InstanceID identifies the provider robot instance for sending.
	InstanceID int64 `json:"instanceID"`
	// RoomID is the provider group identifier.
	RoomID string `json:"roomID"`
	// PushState is ready, blocked, or unknown.
	PushState string `json:"pushState"`
}

// ListRoomsOutput contains one room page and the provider's total count.
type ListRoomsOutput struct {
	// Page is the requested one-based page.
	Page int `json:"page"`
	// Total is the number of matching provider rooms across all pages.
	Total int `json:"total"`
	// Rooms contains at most twenty room identifiers and states.
	Rooms []Room `json:"rooms"`
}

// ListRoomsOperation reads room targets without changing provider state.
type ListRoomsOperation struct{ client *Client }

// Definition returns the generated room-list contract.
func (ListRoomsOperation) Definition() sdkgo.QueryDefinition { return ListRoomsDefinition }

// Invoke lists one page of notification rooms without returning company names or member counts.
func (operation ListRoomsOperation) Invoke(call sdkgo.Call, input ListRoomsInput) sdkgo.QueryAttempt[ListRoomsOutput] {
	if input.Page < 1 || input.Page > 1000000 {
		return roomsBranch(ListRoomsBranchDefect, sdkgo.FailureValidation, "Likelic requires a page number from 1 to 1000000")
	}
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !isValidCustomerCode(credentials.CustomerCode) || !isValidAPIKey(credentials.APIKey.Reveal()) {
		return roomsBranch(ListRoomsBranchDefect, sdkgo.FailureAuthentication, "Likelic credentials are unavailable or invalid")
	}
	endpoint := roomListEndpoint + "?page=" + strconv.Itoa(input.Page) + "&limit=20"
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, endpoint, nil)
	if err != nil {
		return roomsBranch(ListRoomsBranchDefect, sdkgo.FailureValidation, "Likelic room request is invalid")
	}
	request.Header.Set("X-Customer-Code", credentials.CustomerCode)
	request.Header.Set("X-API-Key", credentials.APIKey.Reveal())
	request.Header.Set("Accept", "application/json")
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return roomsRetry(sdkgo.FailureAvailability, "Likelic room request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests {
		return roomsRetry(sdkgo.FailureRateLimit, "Likelic rate limited the room request", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		return roomsRetry(sdkgo.FailureAvailability, "Likelic room service is temporarily unavailable", retryAfter)
	}
	if response.StatusCode != http.StatusOK {
		return classifyRoomsHTTPStatus(response.StatusCode)
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return roomsBranch(ListRoomsBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Likelic response exceeded the configured limit")
	}
	if err != nil {
		return roomsRetry(sdkgo.FailureAvailability, "Likelic response was interrupted", 0)
	}
	var wire struct {
		Success *bool `json:"success"`
		Data    []struct {
			Channel    string `json:"channel"`
			InstanceID int64  `json:"instance_id"`
			RoomID     string `json:"room_id"`
			PushState  string `json:"push_state"`
		} `json:"data"`
		Meta *struct {
			Page  int `json:"page"`
			Limit int `json:"limit"`
			Total int `json:"total"`
		} `json:"meta"`
	}
	if json.Unmarshal(body, &wire) != nil || wire.Success == nil || !*wire.Success || wire.Data == nil || wire.Meta == nil || wire.Meta.Page != input.Page || wire.Meta.Limit != 20 || wire.Meta.Total < 0 || len(wire.Data) > 20 {
		return roomsBranch(ListRoomsBranchInvalidResponse, sdkgo.FailureProtocol, "Likelic returned invalid room page")
	}
	rooms := make([]Room, 0, len(wire.Data))
	for _, item := range wire.Data {
		if item.Channel != "wework" && item.Channel != "qq" || item.InstanceID <= 0 || item.RoomID == "" || len(item.RoomID) > 256 || strings.ContainsAny(item.RoomID, "\r\n\x00") || item.PushState != "ready" && item.PushState != "blocked" && item.PushState != "unknown" {
			return roomsBranch(ListRoomsBranchInvalidResponse, sdkgo.FailureProtocol, "Likelic returned invalid room data")
		}
		rooms = append(rooms, Room{Channel: item.Channel, InstanceID: item.InstanceID, RoomID: item.RoomID, PushState: item.PushState})
	}
	return sdkgo.NewQueryBranch(ListRoomsBranchFound, ListRoomsOutput{Page: input.Page, Total: wire.Meta.Total, Rooms: rooms}, nil, sdkgo.Receipt{})
}
func classifyRoomsHTTPStatus(status int) sdkgo.QueryAttempt[ListRoomsOutput] {
	kind := sdkgo.FailureProviderRejection
	if status == http.StatusUnauthorized {
		kind = sdkgo.FailureAuthentication
	}
	if status == http.StatusForbidden {
		kind = sdkgo.FailureAuthorization
	}
	return roomsBranch(ListRoomsBranchProviderRejected, kind, "Likelic rejected room request with HTTP "+strconv.Itoa(status))
}
func roomsBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[ListRoomsOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "likelic", Operation: "listRooms", Message: message}
	return sdkgo.NewQueryBranch(branch, ListRoomsOutput{}, &failure, sdkgo.Receipt{})
}
func roomsRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[ListRoomsOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "likelic", Operation: "listRooms", Message: message}
	return sdkgo.NewQueryRetry[ListRoomsOutput](failure, delay)
}

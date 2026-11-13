// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package quanshi

import (
	"crypto/rand"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const tokenPath = "/eventopenapi/token/v2/create"
const roomsPath = "/confopenapi/room/list"

// ListRoomsInput requests the rooms visible to the administrator derived from the token.
type ListRoomsInput struct{}

// RoomSummary contains only operational room metadata.
type RoomSummary struct {
	// ID is the stable meeting room identifier.
	ID int64 `json:"id"`
	// Name is the meeting room name.
	Name string `json:"name"`
	// Capacity is the number of people supported by the room.
	Capacity int `json:"capacity"`
	// Status is the provider's roomStatus numeric value.
	Status int `json:"status"`
}

// ListRoomsOutput contains the returned meeting rooms.
type ListRoomsOutput struct {
	// Rooms are returned in provider order.
	Rooms []RoomSummary `json:"rooms"`
}

// ListRoomsOperation reads Quanshi meeting rooms.
type ListRoomsOperation struct{ client *Client }

// Definition returns the generated room-list Query contract.
func (ListRoomsOperation) Definition() sdkgo.QueryDefinition { return ListRoomsDefinition }

// Invoke creates a short-lived token and lists rooms for its administrator userId.
func (operation ListRoomsOperation) Invoke(call sdkgo.Call, _ ListRoomsInput) sdkgo.QueryAttempt[ListRoomsOutput] {
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !isValidCredential(credentials.AppID.Reveal()) || !isValidCredential(credentials.AppSecret.Reveal()) {
		return roomBranch(ListRoomsBranchDefect, sdkgo.FailureAuthentication, "Quanshi credentials are unavailable or invalid")
	}
	var nonceBytes [16]byte
	if _, err := rand.Read(nonceBytes[:]); err != nil {
		return roomRetry(sdkgo.FailureAvailability, "request nonce could not be generated", 0)
	}
	nonce := hex.EncodeToString(nonceBytes[:])
	curTime := strconv.FormatInt(time.Now().UnixMilli(), 10)
	digest := sha1.Sum([]byte(credentials.AppSecret.Reveal() + nonce + curTime))
	checksum := hex.EncodeToString(digest[:])
	tokenRequest, err := http.NewRequestWithContext(call.Context, http.MethodPost, operation.client.baseURL+tokenPath, strings.NewReader("{}"))
	if err != nil {
		return roomBranch(ListRoomsBranchDefect, sdkgo.FailureValidation, "token request is invalid")
	}
	tokenRequest.Header.Set("Accept", "application/json")
	tokenRequest.Header.Set("Content-Type", "application/json")
	tokenRequest.Header.Set("AppId", credentials.AppID.Reveal())
	tokenRequest.Header.Set("Nonce", nonce)
	tokenRequest.Header.Set("CurTime", curTime)
	tokenRequest.Header.Set("CheckSum", checksum)
	tokenResponse, err := operation.client.httpClient.Do(tokenRequest)
	if err != nil {
		return roomRetry(sdkgo.FailureAvailability, "Quanshi token transport failed", 0)
	}
	tokenBody, attempt := operation.readResponse(tokenResponse, "token")
	if attempt != nil {
		return *attempt
	}
	var tokenWire struct {
		Code *int `json:"code"`
		Data *struct {
			Token  string `json:"token"`
			UserID int64  `json:"userId"`
		} `json:"data"`
	}
	if !decodeOne(tokenBody, &tokenWire) || tokenWire.Code == nil {
		return roomBranch(ListRoomsBranchInvalidResponse, sdkgo.FailureProtocol, "Quanshi returned invalid token data")
	}
	if *tokenWire.Code != 200 {
		return classifyQuanshiCode(*tokenWire.Code, "token")
	}
	if tokenWire.Data == nil || !isValidCredential(tokenWire.Data.Token) || tokenWire.Data.UserID <= 0 {
		return roomBranch(ListRoomsBranchInvalidResponse, sdkgo.FailureProtocol, "Quanshi returned incomplete token data")
	}
	payload, _ := json.Marshal(struct {
		UserID int64 `json:"userId"`
	}{tokenWire.Data.UserID})
	roomRequest, err := http.NewRequestWithContext(call.Context, http.MethodPost, operation.client.baseURL+roomsPath, strings.NewReader(string(payload)))
	if err != nil {
		return roomBranch(ListRoomsBranchDefect, sdkgo.FailureValidation, "room request is invalid")
	}
	roomRequest.Header.Set("Accept", "application/json")
	roomRequest.Header.Set("Content-Type", "application/json")
	roomRequest.Header.Set("token", tokenWire.Data.Token)
	roomResponse, err := operation.client.httpClient.Do(roomRequest)
	if err != nil {
		return roomRetry(sdkgo.FailureAvailability, "Quanshi room transport failed", 0)
	}
	body, attempt := operation.readResponse(roomResponse, "room")
	if attempt != nil {
		return *attempt
	}
	var wire struct {
		Status *int `json:"status"`
		Result *struct {
			Room []struct {
				RoomID       int64  `json:"roomId"`
				RoomName     string `json:"roomName"`
				RoomCapacity int    `json:"roomCapacity"`
				RoomStatus   *int   `json:"roomStatus"`
			} `json:"room"`
		} `json:"result"`
	}
	if !decodeOne(body, &wire) || wire.Status == nil {
		return roomBranch(ListRoomsBranchInvalidResponse, sdkgo.FailureProtocol, "Quanshi returned invalid room data")
	}
	if *wire.Status != 0 {
		return classifyQuanshiCode(*wire.Status, "room")
	}
	if wire.Result == nil || wire.Result.Room == nil || len(wire.Result.Room) > 1000 {
		return roomBranch(ListRoomsBranchInvalidResponse, sdkgo.FailureProtocol, "Quanshi returned invalid room list")
	}
	rooms := make([]RoomSummary, 0, len(wire.Result.Room))
	for _, item := range wire.Result.Room {
		if item.RoomID <= 0 || item.RoomName == "" || len(item.RoomName) > 512 || item.RoomCapacity < 0 || item.RoomStatus == nil {
			return roomBranch(ListRoomsBranchInvalidResponse, sdkgo.FailureProtocol, "Quanshi returned invalid room metadata")
		}
		rooms = append(rooms, RoomSummary{ID: item.RoomID, Name: item.RoomName, Capacity: item.RoomCapacity, Status: *item.RoomStatus})
	}
	return sdkgo.NewQueryBranch(ListRoomsBranchListed, ListRoomsOutput{Rooms: rooms}, nil, sdkgo.Receipt{})
}
func (operation ListRoomsOperation) readResponse(response *http.Response, stage string) ([]byte, *sdkgo.QueryAttempt[ListRoomsOutput]) {
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests {
		attempt := roomRetry(sdkgo.FailureRateLimit, "Quanshi "+stage+" rate limited", retryAfter)
		return nil, &attempt
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		attempt := roomRetry(sdkgo.FailureAvailability, "Quanshi "+stage+" temporarily unavailable", retryAfter)
		return nil, &attempt
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		attempt := roomBranch(ListRoomsBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Quanshi "+stage+" response exceeded the configured limit")
		return nil, &attempt
	}
	if err != nil {
		attempt := roomRetry(sdkgo.FailureAvailability, "Quanshi "+stage+" response was interrupted", 0)
		return nil, &attempt
	}
	if response.StatusCode != http.StatusOK {
		kind := sdkgo.FailureProviderRejection
		if response.StatusCode == http.StatusUnauthorized {
			kind = sdkgo.FailureAuthentication
		}
		if response.StatusCode == http.StatusForbidden {
			kind = sdkgo.FailureAuthorization
		}
		attempt := roomBranch(ListRoomsBranchProviderRejected, kind, "Quanshi rejected "+stage+" with HTTP "+strconv.Itoa(response.StatusCode))
		return nil, &attempt
	}
	return body, nil
}
func classifyQuanshiCode(code int, stage string) sdkgo.QueryAttempt[ListRoomsOutput] {
	if code == 9004 {
		return roomBranch(ListRoomsBranchProviderRejected, sdkgo.FailureAuthentication, "Quanshi "+stage+" token expired")
	}
	if code == 9020 {
		return roomBranch(ListRoomsBranchProviderRejected, sdkgo.FailureAuthorization, "Quanshi "+stage+" tenant denied")
	}
	return roomBranch(ListRoomsBranchProviderRejected, sdkgo.FailureProviderRejection, "Quanshi rejected "+stage+" with code "+strconv.Itoa(code))
}
func decodeOne(body []byte, target any) bool {
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	if decoder.Decode(target) != nil {
		return false
	}
	var extra any
	return decoder.Decode(&extra) == io.EOF
}
func isValidCredential(value string) bool {
	if value == "" || len(value) > 512 {
		return false
	}
	for _, character := range value {
		if character < 0x21 || character > 0x7e {
			return false
		}
	}
	return true
}
func roomBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[ListRoomsOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "quanshi", Operation: "listRooms", Message: message}
	return sdkgo.NewQueryBranch(branch, ListRoomsOutput{}, &failure, sdkgo.Receipt{})
}
func roomRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[ListRoomsOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "quanshi", Operation: "listRooms", Message: message}
	return sdkgo.NewQueryRetry[ListRoomsOutput](failure, delay)
}

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package quanshi

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

const testAppID = "test-app-id"
const testAppSecret = "private-test-secret"

var testConnection = sdkgo.ConnectionRef{Provider: "quanshi", Name: "quanshi"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "quanshi-provider-test" }
func (queryContext) StepExecutionID() string     { return "room-list" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }

type redirectTransport struct{ serverURL *url.URL }

func (transport redirectTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	cloned := request.Clone(request.Context())
	cloned.URL.Scheme = transport.serverURL.Scheme
	cloned.URL.Host = transport.serverURL.Host
	return http.DefaultTransport.RoundTrip(cloned)
}
func newTestClient(t *testing.T, server *httptest.Server, maxResponseBytes int64) *Client {
	t.Helper()
	serverURL, err := url.Parse(server.URL)
	require.NoError(t, err)
	credentials := sdkgo.StaticCredentialProvider[Credentials]{testConnection: {AppID: sdkgo.NewSecretString(testAppID), AppSecret: sdkgo.NewSecretString(testAppSecret)}}
	client, err := New(Config{BaseURL: "https://openapi.quanshicloud.com", MaxResponseBytes: maxResponseBytes}, credentials, WithHTTPClient(&http.Client{Transport: redirectTransport{serverURL}}))
	require.NoError(t, err)
	return client
}
func TestListRoomsTokenSignatureDerivedUserAndPrivacy(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls++
		switch request.URL.Path {
		case tokenPath:
			require.Equal(t, http.MethodPost, request.Method)
			require.Equal(t, testAppID, request.Header.Get("AppId"))
			require.NotEmpty(t, request.Header.Get("Nonce"))
			require.NotEmpty(t, request.Header.Get("CurTime"))
			digest := sha1.Sum([]byte(testAppSecret + request.Header.Get("Nonce") + request.Header.Get("CurTime")))
			require.Equal(t, hex.EncodeToString(digest[:]), request.Header.Get("CheckSum"))
			_, err := writer.Write([]byte(`{"code":200,"data":{"token":"short-lived-token","userId":88408809,"userName":"private"}}`))
			require.NoError(t, err)
		case roomsPath:
			require.Equal(t, http.MethodPost, request.Method)
			require.Equal(t, "short-lived-token", request.Header.Get("token"))
			var body map[string]int64
			require.NoError(t, json.NewDecoder(request.Body).Decode(&body))
			require.Equal(t, int64(88408809), body["userId"])
			_, err := writer.Write([]byte(`{"status":0,"result":{"room":[{"roomId":10935,"roomName":"Example Room","roomCapacity":20,"roomStatus":1,"roomPhone":"private-phone","manager":{"email":"private@example.com"}}],"lastApprovalRoom":{},"equip":[]}}`))
			require.NoError(t, err)
		default:
			t.Errorf("unexpected path %s", request.URL.Path)
		}
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server, 0).ListRooms(), testConnection, ListRoomsInput{})
	require.NoError(t, err)
	require.Equal(t, ListRoomsBranchListed, result.Branch)
	require.Equal(t, 2, calls)
	require.Equal(t, ListRoomsOutput{Rooms: []RoomSummary{{ID: 10935, Name: "Example Room", Capacity: 20, Status: 1}}}, result.Value)
	encoded, err := json.Marshal(result.Value)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "private")
	require.NotContains(t, string(encoded), "short-lived-token")
}
func TestListRoomsFailures(t *testing.T) {
	cases := []struct {
		name        string
		tokenStatus int
		tokenBody   string
		roomStatus  int
		roomBody    string
		branch      sdkgo.BranchID
		kind        sdkgo.FailureKind
		retry       bool
	}{
		{"bad app", 403, "private", 0, "", ListRoomsBranchProviderRejected, sdkgo.FailureAuthorization, false},
		{"token rate limit", 429, "private", 0, "", "", sdkgo.FailureRateLimit, true},
		{"token malformed", 200, "{}", 0, "", ListRoomsBranchInvalidResponse, sdkgo.FailureProtocol, false},
		{"token rejected", 200, `{"code":9004}`, 0, "", ListRoomsBranchProviderRejected, sdkgo.FailureAuthentication, false},
		{"room forbidden", 200, `{"code":200,"data":{"token":"token","userId":1}}`, 403, "private", ListRoomsBranchProviderRejected, sdkgo.FailureAuthorization, false},
		{"room unavailable", 200, `{"code":200,"data":{"token":"token","userId":1}}`, 503, "private", "", sdkgo.FailureAvailability, true},
		{"room tenant mismatch", 200, `{"code":200,"data":{"token":"token","userId":1}}`, 200, `{"status":9020}`, ListRoomsBranchProviderRejected, sdkgo.FailureAuthorization, false},
		{"room malformed", 200, `{"code":200,"data":{"token":"token","userId":1}}`, 200, "{}", ListRoomsBranchInvalidResponse, sdkgo.FailureProtocol, false},
		{"missing room status", 200, `{"code":200,"data":{"token":"token","userId":1}}`, 200, `{"status":0,"result":{"room":[{"roomId":1,"roomName":"A","roomCapacity":20}]}}`, ListRoomsBranchInvalidResponse, sdkgo.FailureProtocol, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				writer.Header().Set("Retry-After", "2")
				if request.URL.Path == tokenPath {
					writer.WriteHeader(tc.tokenStatus)
					_, err := writer.Write([]byte(tc.tokenBody))
					require.NoError(t, err)
				} else {
					writer.WriteHeader(tc.roomStatus)
					_, err := writer.Write([]byte(tc.roomBody))
					require.NoError(t, err)
				}
			}))
			defer server.Close()
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server, 0).ListRooms(), testConnection, ListRoomsInput{})
			if tc.retry {
				var retry *dex.RetryAfterError
				require.ErrorAs(t, err, &retry)
				require.Equal(t, 2*time.Second, retry.After)
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.branch, result.Branch)
				require.Equal(t, tc.kind, result.Failure.Kind)
				require.NotContains(t, result.Failure.Message, testAppSecret)
			}
		})
	}
}
func TestListRoomsSizeTransportAndURLValidation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, err := writer.Write([]byte(`{"code":200,"data":{"token":"token","userId":1}}`))
		require.NoError(t, err)
	}))
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server, 8).ListRooms(), testConnection, ListRoomsInput{})
	require.NoError(t, err)
	require.Equal(t, ListRoomsBranchInvalidResponse, result.Branch)
	server.Close()
	_, err = sdkgo.RunQuery(queryContext{}, newTestClient(t, server, 0).ListRooms(), testConnection, ListRoomsInput{})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	credentials := sdkgo.StaticCredentialProvider[Credentials]{testConnection: {AppID: sdkgo.NewSecretString(testAppID), AppSecret: sdkgo.NewSecretString(testAppSecret)}}
	_, err = New(Config{BaseURL: "http://openapi.quanshicloud.com"}, credentials)
	require.Error(t, err)
	_, err = New(Config{BaseURL: "https://other.example"}, credentials)
	require.Error(t, err)
}

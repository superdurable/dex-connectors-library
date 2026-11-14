// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package bilibili

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

var testConnection = sdkgo.ConnectionRef{Provider: "bilibili", Name: "bilibili"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "bilibili-provider-test" }
func (queryContext) StepExecutionID() string     { return "get-authorized-user" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }
func newTestClient(t *testing.T, endpoint string, maxBytes int64) *Client {
	t.Helper()
	credentials := Credentials{ClientID: "client123", AppSecret: sdkgo.NewSecretString("secret456"), AccessToken: sdkgo.NewSecretString("token789")}
	client, err := New(Config{BaseURL: endpoint, MaxResponseBytes: maxBytes}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials})
	require.NoError(t, err)
	return client
}
func TestOfficialSignatureConstruction(t *testing.T) {
	headers := map[string]string{"x-bili-accesskeyid": "client123", "x-bili-content-md5": "d41d8cd98f00b204e9800998ecf8427e", "x-bili-signature-method": "HMAC-SHA256", "x-bili-signature-nonce": "nonce456", "x-bili-signature-version": "2.0", "x-bili-timestamp": "1730185575"}
	canonical := "x-bili-accesskeyid:client123\nx-bili-content-md5:d41d8cd98f00b204e9800998ecf8427e\nx-bili-signature-method:HMAC-SHA256\nx-bili-signature-nonce:nonce456\nx-bili-signature-version:2.0\nx-bili-timestamp:1730185575"
	signer := hmac.New(sha256.New, []byte("secret456"))
	_, err := signer.Write([]byte(canonical))
	require.NoError(t, err)
	require.Equal(t, hex.EncodeToString(signer.Sum(nil)), signRequest("secret456", headers))
}
func TestGetAuthorizedUserSignsRequestAndOmitsAvatar(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, "/arcopen/fn/user/account/info", request.URL.Path)
		require.Empty(t, request.URL.RawQuery)
		require.Equal(t, "application/json", request.Header.Get("Accept"))
		require.Equal(t, "application/json", request.Header.Get("Content-Type"))
		require.Equal(t, "token789", request.Header.Get("Access-Token"))
		require.Equal(t, "client123", request.Header.Get("X-Bili-Accesskeyid"))
		require.Equal(t, "d41d8cd98f00b204e9800998ecf8427e", request.Header.Get("X-Bili-Content-Md5"))
		require.Equal(t, "HMAC-SHA256", request.Header.Get("X-Bili-Signature-Method"))
		require.Equal(t, "2.0", request.Header.Get("X-Bili-Signature-Version"))
		require.Len(t, request.Header.Get("X-Bili-Signature-Nonce"), 32)
		timestamp, err := strconv.ParseInt(request.Header.Get("X-Bili-Timestamp"), 10, 64)
		require.NoError(t, err)
		require.WithinDuration(t, time.Now(), time.Unix(timestamp, 0), 5*time.Second)
		headers := map[string]string{}
		for _, name := range []string{"x-bili-accesskeyid", "x-bili-content-md5", "x-bili-signature-method", "x-bili-signature-nonce", "x-bili-signature-version", "x-bili-timestamp"} {
			headers[name] = request.Header.Get(name)
		}
		require.Equal(t, signRequest("secret456", headers), request.Header.Get("Authorization"))
		_, err = io.WriteString(writer, `{"code":0,"data":{"face":"https://i0.hdslb.com/bfs/face/sample.jpg","name":"sample_creator","openid":"fc9899b46ff443cea38190d355d49f3a"}}`)
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).GetAuthorizedUser(), testConnection, GetAuthorizedUserInput{})
	require.NoError(t, err)
	require.Equal(t, GetAuthorizedUserBranchFound, result.Branch)
	require.Equal(t, AuthorizedUser{OpenID: "fc9899b46ff443cea38190d355d49f3a", Name: "sample_creator"}, result.Value)
}
func TestGetAuthorizedUserClassifiesOutcomes(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{"permission", 200, `{"code":127007}`, GetAuthorizedUserBranchProviderRejected, false},
		{"token", 200, `{"code":127001}`, GetAuthorizedUserBranchProviderRejected, false},
		{"provider limit", 200, `{"code":127306}`, "", true},
		{"provider busy", 200, `{"code":122009}`, "", true},
		{"http limit", 429, `{}`, "", true},
		{"unavailable", 503, `{}`, "", true},
		{"missing data", 200, `{"code":0}`, GetAuthorizedUserBranchInvalidResponse, false},
		{"missing name", 200, `{"code":0,"data":{"openid":"x"}}`, GetAuthorizedUserBranchInvalidResponse, false},
		{"malformed", 200, `{"code":`, GetAuthorizedUserBranchInvalidResponse, false},
		{"trailing", 200, `{"code":0,"data":{"openid":"x","name":"y"}} {}`, GetAuthorizedUserBranchInvalidResponse, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(tc.status)
				_, err := io.WriteString(writer, tc.body)
				require.NoError(t, err)
			}))
			defer server.Close()
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).GetAuthorizedUser(), testConnection, GetAuthorizedUserInput{})
			if tc.retry {
				var retry *sdkgo.RetryError
				require.ErrorAs(t, err, &retry)
				require.NotContains(t, err.Error(), "secret456")
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.branch, result.Branch)
		})
	}
}
func TestGetAuthorizedUserBoundsResponseAndChecksCredentials(t *testing.T) {
	client, err := New(Config{}, sdkgo.StaticCredentialProvider[Credentials]{})
	require.NoError(t, err)
	result, err := sdkgo.RunQuery(queryContext{}, client.GetAuthorizedUser(), testConnection, GetAuthorizedUserInput{})
	require.NoError(t, err)
	require.Equal(t, GetAuthorizedUserBranchDefect, result.Branch)
	_, err = New(Config{BaseURL: "https://member.bilibili.com/private"}, sdkgo.StaticCredentialProvider[Credentials]{})
	require.Error(t, err)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, err := io.WriteString(writer, strings.Repeat("x", 100))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err = sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 5).GetAuthorizedUser(), testConnection, GetAuthorizedUserInput{})
	require.NoError(t, err)
	require.Equal(t, GetAuthorizedUserBranchInvalidResponse, result.Branch)
}

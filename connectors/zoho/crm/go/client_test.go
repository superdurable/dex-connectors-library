// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package crm_test

import (
	"context"
	"net/http"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/zoho/crm"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestEveryDataCenterAcceptsOnlyItsOwnZohoapisHosts(t *testing.T) {
	for _, dataCenter := range crm.DataCenters() {
		t.Run(dataCenter.Name, func(t *testing.T) {
			require.Equal(t, "https://www."+dataCenter.APIDomainSuffix, dataCenter.ProductionAPIDomain())
			for _, prefix := range []string{"www.", "sandbox.", "developer."} {
				apiDomain, err := dataCenter.ValidateAPIDomain("https://" + prefix + dataCenter.APIDomainSuffix + "/")
				require.NoError(t, err)
				require.Equal(t, "https://"+prefix+dataCenter.APIDomainSuffix, apiDomain)
			}
			for _, rejected := range []string{
				"http://www." + dataCenter.APIDomainSuffix, "https://www." + dataCenter.APIDomainSuffix + ":8443",
				"https://www." + dataCenter.APIDomainSuffix + "/crm", "https://user@www." + dataCenter.APIDomainSuffix,
				"https://evil.example/www." + dataCenter.APIDomainSuffix, "https://www." + dataCenter.APIDomainSuffix + ".evil.example",
				"https://desk." + dataCenter.APIDomainSuffix, "https://www.zohoapis.com.cn",
			} {
				_, err := dataCenter.ValidateAPIDomain(rejected)
				require.Error(t, err, rejected)
			}
			for _, other := range crm.DataCenters() {
				if other.AuthMethodID != dataCenter.AuthMethodID {
					_, err := dataCenter.ValidateAPIDomain(other.ProductionAPIDomain())
					require.Error(t, err, "a token is never sent to another data center's host")
				}
			}
		})
	}
}

func TestRequestsGoToTheStoredSandboxAPIDomainOfTheDataCenter(t *testing.T) {
	provider := newRecordingCRM(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeValue(t, response, http.StatusOK, map[string]any{"data": []any{dealJSON(testDealID, "Qualification", "2026-01-28T18:30:00+05:30")}})
	})
	credentials := testCredentials(crm.EUDataCenterAuthMethodID)
	credentials.APIDomain = "https://sandbox.zohoapis.eu"
	client, err := crm.New(crm.Config{}, sdkgo.StaticCredentialProvider[crm.Credentials]{crmConnection: credentials},
		crm.WithHTTPClient(routingClient(t, provider.Server, "sandbox.zohoapis.eu")))
	require.NoError(t, err)
	result, err := sdkgo.RunQuery(newCRMDexContext("get"), client.GetRecord(), crmConnection, crm.GetRecordInput{Module: crm.ModuleDeals, RecordID: testDealID})
	require.NoError(t, err)
	require.Equal(t, crm.GetRecordBranchFound, result.Branch)
	require.Equal(t, "sandbox.zohoapis.eu", provider.request(0).host)
	require.Equal(t, "/crm/v8/Deals/"+testDealID, provider.request(0).path)
}

func TestUnusableCredentialsSelectDefectWithoutARequest(t *testing.T) {
	provider := newRecordingCRM(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, `{}`)
	})
	for name, change := range map[string]func(*crm.Credentials){
		"api_domain of another data center": func(credentials *crm.Credentials) { credentials.APIDomain = "https://www.zohoapis.eu" },
		"unsupported data center":           func(credentials *crm.Credentials) { credentials.AuthMethodID = "zoho-cn-oauth" },
		"token with a line break":           func(credentials *crm.Credentials) { credentials.AccessToken = sdkgo.NewSecretString("1000.a\nb") },
	} {
		credentials := testCredentials(crm.USDataCenterAuthMethodID)
		change(&credentials)
		client, err := crm.New(crm.Config{}, sdkgo.StaticCredentialProvider[crm.Credentials]{crmConnection: credentials},
			crm.WithHTTPClient(routingClient(t, provider.Server, "www.zohoapis.com", "www.zohoapis.eu")))
		require.NoError(t, err)
		result, err := sdkgo.RunQuery(newCRMDexContext("get"), client.GetRecord(), crmConnection, crm.GetRecordInput{Module: crm.ModuleDeals, RecordID: testDealID})
		require.NoError(t, err, name)
		require.Equal(t, crm.GetRecordBranchDefect, result.Branch, name)
		require.Equal(t, sdkgo.FailureAuthentication, result.Failure.Kind, name)
	}
	require.Zero(t, provider.requestCount())
}

// rejectionRefreshingCredentialProvider hands out a replacement token after a provider rejection.
type rejectionRefreshingCredentialProvider struct {
	mutex           sync.Mutex
	forcedRefreshes int
}

func (*rejectionRefreshingCredentialProvider) Resolve(sdkgo.Call) (crm.Credentials, error) {
	credentials := testCredentials(crm.USDataCenterAuthMethodID)
	credentials.AccessToken = sdkgo.NewSecretString("1000.rejected-token")
	return credentials, nil
}

// ResolveWithRefresh returns the token Resolve returns, which has not expired before Zoho CRM rejects it.
func (provider *rejectionRefreshingCredentialProvider) ResolveWithRefresh(
	_ context.Context,
	call sdkgo.Call,
	_ sdkgo.CredentialRefreshDriver[crm.Credentials],
) (crm.Credentials, error) {
	return provider.Resolve(call)
}

func (provider *rejectionRefreshingCredentialProvider) ResolveAfterRejection(
	context.Context,
	sdkgo.Call,
	sdkgo.CredentialRefreshDriver[crm.Credentials],
) (crm.Credentials, error) {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	provider.forcedRefreshes++
	credentials := testCredentials(crm.USDataCenterAuthMethodID)
	credentials.AccessToken = sdkgo.NewSecretString("1000.replacement-token")
	return credentials, nil
}

func TestAnInvalidTokenIsRefreshedOnceAndTheRequestResent(t *testing.T) {
	provider := newRecordingCRM(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		if request.Header.Get("Authorization") != "Zoho-oauthtoken 1000.replacement-token" {
			writeJSON(t, response, http.StatusUnauthorized, `{"code":"INVALID_TOKEN","details":{},"message":"SENTINEL invalid oauth token","status":"error"}`)
			return
		}
		writeValue(t, response, http.StatusOK, writeSuccessJSON("", testDealID, nil))
	})
	credentials := &rejectionRefreshingCredentialProvider{}
	client, err := crm.New(crm.Config{}, credentials, crm.WithAPIBaseURL(provider.URL+"/crm/v8"))
	require.NoError(t, err)
	result, err := sdkgo.RunMutation(newCRMDexContext("update"), client.UpdateRecord(), crmConnection, dealStageUpdateInput())
	require.NoError(t, err)
	require.Equal(t, crm.UpdateRecordBranchUpdated, result.Branch)
	require.Equal(t, 1, credentials.forcedRefreshes)
	require.Equal(t, 2, provider.requestCount(), "Zoho CRM rejects an invalid token before applying, so one resend is safe")
}

func TestAScopeMismatchIsNotRefreshedAndASecond401IsRejected(t *testing.T) {
	for name, scenario := range map[string]struct {
		body             string
		expectedRequests int
		expectedRefresh  int
		kind             sdkgo.FailureKind
	}{
		"scope mismatch": {`{"code":"OAUTH_SCOPE_MISMATCH","details":{},"message":"SENTINEL","status":"error"}`, 1, 0, sdkgo.FailureAuthorization},
		"invalid twice":  {`{"code":"INVALID_TOKEN","details":{},"message":"SENTINEL","status":"error"}`, 2, 1, sdkgo.FailureAuthentication},
	} {
		t.Run(name, func(t *testing.T) {
			provider := newRecordingCRM(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
				writeJSON(t, response, http.StatusUnauthorized, scenario.body)
			})
			credentials := &rejectionRefreshingCredentialProvider{}
			client, err := crm.New(crm.Config{}, credentials, crm.WithAPIBaseURL(provider.URL+"/crm/v8"))
			require.NoError(t, err)
			result, err := sdkgo.RunQuery(newCRMDexContext("get"), client.GetRecord(), crmConnection, crm.GetRecordInput{Module: crm.ModuleDeals, RecordID: testDealID})
			require.NoError(t, err)
			require.Equal(t, crm.GetRecordBranchProviderRejected, result.Branch)
			require.Equal(t, scenario.kind, result.Failure.Kind)
			require.Equal(t, scenario.expectedRequests, provider.requestCount())
			require.Equal(t, scenario.expectedRefresh, credentials.forcedRefreshes)
			requireNoSentinel(t, result)
		})
	}
}

func TestNewRejectsInvalidConfigurationAndOptions(t *testing.T) {
	_, err := crm.New(crm.Config{MaxResponseBytes: -1}, testCredentialProvider())
	require.Error(t, err)
	_, err = crm.New(crm.Config{}, nil)
	require.Error(t, err)
	_, err = crm.New(crm.Config{}, testCredentialProvider(), nil)
	require.Error(t, err)
	_, err = crm.New(crm.Config{}, testCredentialProvider(), crm.WithAPIBaseURL("http://crm.example.com/crm/v8"))
	require.Error(t, err, "a non-loopback override must use HTTPS")
	require.Equal(t, int64(4194304), crm.DefaultConfig().MaxResponseBytes)
}

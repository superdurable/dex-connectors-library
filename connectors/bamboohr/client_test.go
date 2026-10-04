// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package bamboohr_test

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/bamboohr"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

type failingCredentialProvider struct{}

func (failingCredentialProvider) Resolve(sdkgo.Call) (bamboohr.Credentials, error) {
	return bamboohr.Credentials{}, errors.New("SENTINEL credential source detail")
}

func TestNewValidatesTheCompanyDomainAndOptions(t *testing.T) {
	for _, domain := range []string{"", "https://acme.bamboohr.com", "acme.bamboohr.com", "Acme", "-acme", "acme_hr"} {
		_, err := bamboohr.New(bamboohr.Config{CompanyDomain: domain}, testCredentialProvider())
		require.Error(t, err, domain)
	}
	_, err := bamboohr.New(bamboohr.Config{CompanyDomain: "acme-hr2"}, testCredentialProvider())
	require.NoError(t, err)
	_, err = bamboohr.New(bamboohr.Config{CompanyDomain: "acme"}, nil)
	require.Error(t, err)
	_, err = bamboohr.New(bamboohr.Config{CompanyDomain: "acme"}, testCredentialProvider(), nil)
	require.Error(t, err)
	_, err = bamboohr.New(bamboohr.Config{CompanyDomain: "acme", MaxResponseBytes: -1}, testCredentialProvider())
	require.Error(t, err)
	for _, baseURL := range []string{"http://example.com/api/v1", "https://user:pass@example.com/api/v1", "https://example.com/api/v1?x=1"} {
		_, err = bamboohr.New(bamboohr.Config{CompanyDomain: "acme"}, testCredentialProvider(), bamboohr.WithAPIBaseURL(baseURL))
		require.Error(t, err, baseURL)
	}
	require.Equal(t, int64(4194304), bamboohr.DefaultConfig().MaxResponseBytes)
}

func TestUnusableCredentialsSelectDefectWithoutARequest(t *testing.T) {
	provider := newRecordingBambooHR(t, func(http.ResponseWriter, *http.Request, int) { t.Fatal("no request is expected") })
	for name, credentials := range map[string]sdkgo.CredentialProvider[bamboohr.Credentials]{
		"credential source failure": failingCredentialProvider{},
		"colon in key":              sdkgo.StaticCredentialProvider[bamboohr.Credentials]{bambooHRConnection: {APIKey: sdkgo.NewSecretString("abc:def")}},
		"space in key":              sdkgo.StaticCredentialProvider[bamboohr.Credentials]{bambooHRConnection: {APIKey: sdkgo.NewSecretString("abc def")}},
		"empty key":                 sdkgo.StaticCredentialProvider[bamboohr.Credentials]{bambooHRConnection: {}},
	} {
		t.Run(name, func(t *testing.T) {
			client, err := bamboohr.New(bamboohr.Config{CompanyDomain: testCompanyDomain}, credentials, bamboohr.WithAPIBaseURL(provider.URL+"/api/v1"))
			require.NoError(t, err)
			result, err := sdkgo.RunQuery(newBambooHRDexContext("credentials"), client.GetEmployee(), bambooHRConnection,
				bamboohr.GetEmployeeInput{EmployeeID: "123", Fields: []string{"firstName"}})
			require.NoError(t, err)
			require.Equal(t, bamboohr.GetEmployeeBranchDefect, result.Branch)
			require.Equal(t, sdkgo.FailureAuthentication, result.Failure.Kind)
			require.NotContains(t, result.Failure.Message, "SENTINEL")
		})
	}
}

func TestOversizedResponsesSelectInvalidResponse(t *testing.T) {
	provider := newRecordingBambooHR(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, `{"id":"123","firstName":"`+strings.Repeat("a", 512)+`"}`)
	})
	client, err := bamboohr.New(bamboohr.Config{CompanyDomain: testCompanyDomain, MaxResponseBytes: 256}, testCredentialProvider(),
		bamboohr.WithAPIBaseURL(provider.URL+"/api/v1"))
	require.NoError(t, err)
	result, err := sdkgo.RunQuery(newBambooHRDexContext("oversized"), client.GetEmployee(), bambooHRConnection,
		bamboohr.GetEmployeeInput{EmployeeID: "123", Fields: []string{"firstName"}})
	require.NoError(t, err)
	require.Equal(t, bamboohr.GetEmployeeBranchInvalidResponse, result.Branch)
	require.Equal(t, sdkgo.FailureResponseTooLarge, result.Failure.Kind)
}

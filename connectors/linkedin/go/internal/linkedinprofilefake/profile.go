// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package linkedinprofilefake serves one LinkedIn member's GET /v2/userinfo over HTTP for profileChanged
// tests, in the documented OpenID Connect UserInfo shape: sub, name, given_name, family_name, picture,
// locale, and the optional email and email_verified claims.
package linkedinprofilefake

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// AccessToken is the bearer token the fake accepts; split so it does not look like a credential.
const AccessToken = "linkedin-" + "profile-" + "trigger"

// UserInfoPath is the path the fake serves; tests append it to URL for Config.UserInfoURL.
const UserInfoPath = "/v2/userinfo"

// Profile is one member's UserInfo claims, encoded with LinkedIn's claim names.
type Profile struct {
	Subject       string `json:"sub"`
	Name          string `json:"name,omitempty"`
	GivenName     string `json:"given_name,omitempty"`
	FamilyName    string `json:"family_name,omitempty"`
	Picture       string `json:"picture,omitempty"`
	Locale        string `json:"locale,omitempty"`
	Email         string `json:"email,omitempty"`
	EmailVerified bool   `json:"email_verified,omitempty"`
}

// AdaProfile returns a verified sample member.
func AdaProfile() Profile {
	return Profile{
		Subject: "782bbtaQ", Name: "Ada Lovelace", GivenName: "Ada", FamilyName: "Lovelace",
		Picture: "https://media.licdn.com/dms/image/v2/ada/profile-displayphoto-shrink_100_100/0/1?e=1&v=beta&t=first",
		Locale:  "en-US", Email: "ada@example.com", EmailVerified: true,
	}
}

// UserInfo is a stateful single-member fake that is safe for concurrent use.
type UserInfo struct {
	*httptest.Server
	mutex        sync.Mutex
	profile      Profile
	failures     []failure
	requestCount int
}

type failure struct {
	status     int
	retryAfter string
}

// New starts a fake for profile; the test cleanup closes it.
func New(t *testing.T, profile Profile) *UserInfo {
	t.Helper()
	fake := &UserInfo{profile: profile}
	fake.Server = httptest.NewServer(http.HandlerFunc(fake.serve))
	t.Cleanup(fake.Close)
	return fake
}

// UserInfoURL is the full UserInfo URL for Config.UserInfoURL.
func (fake *UserInfo) UserInfoURL() string { return fake.URL + UserInfoPath }

// Update changes the served profile with edit.
func (fake *UserInfo) Update(edit func(*Profile)) {
	fake.mutex.Lock()
	defer fake.mutex.Unlock()
	edit(&fake.profile)
}

// FailNext answers the next request with status, sending retryAfter as Retry-After when not blank.
// Several calls queue several failures.
func (fake *UserInfo) FailNext(status int, retryAfter string) {
	fake.mutex.Lock()
	defer fake.mutex.Unlock()
	fake.failures = append(fake.failures, failure{status: status, retryAfter: retryAfter})
}

// RequestCount counts UserInfo requests, failed ones included.
func (fake *UserInfo) RequestCount() int {
	fake.mutex.Lock()
	defer fake.mutex.Unlock()
	return fake.requestCount
}

func (fake *UserInfo) serve(writer http.ResponseWriter, request *http.Request) {
	fake.mutex.Lock()
	defer fake.mutex.Unlock()
	if request.Method != http.MethodGet || request.URL.Path != UserInfoPath {
		http.NotFound(writer, request)
		return
	}
	fake.requestCount++
	if request.Header.Get("Authorization") != "Bearer "+AccessToken {
		writer.WriteHeader(http.StatusUnauthorized)
		return
	}
	if len(fake.failures) > 0 {
		next := fake.failures[0]
		fake.failures = fake.failures[1:]
		if next.retryAfter != "" {
			writer.Header().Set("Retry-After", next.retryAfter)
		}
		writer.WriteHeader(next.status)
		return
	}
	writer.Header().Set("Content-Type", "application/json")
	writer.Header().Set("X-LI-UUID", "fake-request")
	// Encoding a struct of strings and a bool cannot fail; a write error means the client left.
	_ = json.NewEncoder(writer).Encode(fake.profile)
}

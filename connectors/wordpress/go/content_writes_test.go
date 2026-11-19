// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package wordpress

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWordPressWriteRefusalRequiresMatchingStatusWithoutAcceptance(t *testing.T) {
	for _, testCase := range []struct {
		name       string
		body       string
		isDefinite bool
	}{
		{name: "provider status", body: `{"code":"rest_forbidden","message":"denied","data":{"status":403}}`, isDefinite: true},
		{name: "validation details", body: `{"code":"rest_forbidden","message":"denied","data":{"status":403,"params":{"title":"invalid"}}}`, isDefinite: true},
		{name: "plain refusal", body: `{"code":"rest_forbidden","message":"denied"}`, isDefinite: true},
		{name: "mismatched status", body: `{"code":"rest_forbidden","message":"denied","data":{"status":200}}`},
		{name: "resource identity", body: `{"id":12,"code":"rest_forbidden","message":"denied","data":{"status":403}}`},
		{name: "alternate identity", body: `{"resource_id":12,"code":"rest_forbidden","message":"denied","data":{"status":403}}`},
		{name: "accepted marker", body: `{"accepted":true,"code":"rest_forbidden","message":"denied","data":{"status":403}}`},
		{name: "resource in data", body: `{"code":"rest_forbidden","message":"denied","data":{"status":403,"id":12}}`},
		{name: "empty", body: `{}`},
		{name: "malformed", body: `{`},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			response := &http.Response{StatusCode: http.StatusForbidden, Body: io.NopCloser(strings.NewReader(testCase.body))}
			require.Equal(t, testCase.isDefinite, isDefiniteWordPressRefusal(response, 1024))
		})
	}
}

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package woocommerce

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWooCommerceWriteRefusalRequiresMatchingStatusWithoutAcceptance(t *testing.T) {
	for _, testCase := range []struct {
		name       string
		body       string
		isDefinite bool
	}{
		{name: "provider status", body: `{"code":"woocommerce_rest_forbidden","message":"denied","data":{"status":403}}`, isDefinite: true},
		{name: "validation details", body: `{"code":"woocommerce_rest_forbidden","message":"denied","data":{"status":403,"params":{"name":"invalid"}}}`, isDefinite: true},
		{name: "plain refusal", body: `{"code":"woocommerce_rest_forbidden","message":"denied"}`, isDefinite: true},
		{name: "mismatched status", body: `{"code":"woocommerce_rest_forbidden","message":"denied","data":{"status":200}}`},
		{name: "resource identity", body: `{"id":12,"code":"woocommerce_rest_forbidden","message":"denied","data":{"status":403}}`},
		{name: "alternate identity", body: `{"resource_id":12,"code":"woocommerce_rest_forbidden","message":"denied","data":{"status":403}}`},
		{name: "accepted marker", body: `{"accepted":true,"code":"woocommerce_rest_forbidden","message":"denied","data":{"status":403}}`},
		{name: "resource in data", body: `{"code":"woocommerce_rest_forbidden","message":"denied","data":{"status":403,"id":12}}`},
		{name: "empty", body: `{}`},
		{name: "malformed", body: `{`},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			response := &http.Response{StatusCode: http.StatusForbidden, Body: io.NopCloser(strings.NewReader(testCase.body))}
			require.Equal(t, testCase.isDefinite, isDefiniteWooCommerceRefusal(response, 1024))
		})
	}
}

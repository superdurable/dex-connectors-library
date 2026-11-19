// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package firestore

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestPutDocumentRetriesMalformedSuccess(t *testing.T) {
	provider := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusOK)
		_, err := response.Write([]byte(`{"name":`))
		require.NoError(t, err)
	}))
	defer provider.Close()
	client := newTestClient(t, provider.URL)
	_, err := sdkgo.RunMutation(queryContext{}, client.PutDocument(), testConnection,
		WriteDocumentInput{ProjectID: "sample-project", CollectionID: "events", DocumentID: "sample-event", FieldsJSON: `{"title":{"stringValue":"Hello"}}`})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
}

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package activecampaign

import (
	"encoding/json"
	"net/http"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

func isDefiniteActiveCampaignRefusal(status int, body []byte) bool {
	switch status {
	case http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound,
		http.StatusLengthRequired, http.StatusRequestEntityTooLarge, http.StatusRequestURITooLong,
		http.StatusUnsupportedMediaType, http.StatusUnprocessableEntity:
	default:
		return false
	}
	var document map[string]json.RawMessage
	if json.Unmarshal(body, &document) != nil {
		return false
	}
	var errors []json.RawMessage
	if json.Unmarshal(document["errors"], &errors) != nil || len(errors) == 0 {
		return false
	}
	for _, key := range []string{"account", "accountContact", "connection", "contact", "deal", "ecomCustomer", "note", "contactList"} {
		if _, hasResult := document[key]; hasResult {
			return false
		}
	}
	return true
}

func limitedActiveCampaignUnknown[T any](call sdkgo.Call, decision singledispatch.Decision, failure sdkgo.Failure, receipt sdkgo.Receipt) sdkgo.MutationAttempt[T] {
	var zero T
	if decision.MaximumPriorSends+1 >= call.MaxSends() {
		return sdkgo.NewMutationUncertain(zero, failure, receipt)
	}
	return sdkgo.NewMutationRetry[T](failure, 0)
}

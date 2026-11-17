// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package elevenlabs

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// ListModelsInput selects the account's available model catalog.
type ListModelsInput struct{}

// ModelMetadata describes a model's speech support and input limit.
type ModelMetadata struct {
	// ModelID is the stable provider model identifier.
	ModelID string `json:"modelId"`
	// Name is the optional provider display name.
	Name string `json:"name"`
	// CanDoTextToSpeech reports whether speech synthesis is supported.
	CanDoTextToSpeech bool `json:"canDoTextToSpeech"`
	// MaximumTextLengthPerRequest is the character limit for one request, or zero when unspecified.
	MaximumTextLengthPerRequest int `json:"maximumTextLengthPerRequest"`
}

// ModelsList contains models available to the connection.
type ModelsList struct {
	// Models contains provider model metadata.
	Models []ModelMetadata `json:"models"`
}

// ListModelsOperation reads the available model catalog.
type ListModelsOperation struct{ client *Client }

// ListModels binds the model catalog Query to this client.
func (client *Client) ListModels() ListModelsOperation { return ListModelsOperation{client} }

// Definition returns the generated model catalog contract.
func (ListModelsOperation) Definition() sdkgo.QueryDefinition { return ListModelsDefinition }

// Invoke reads the provider model catalog without model configuration or prices.
func (operation ListModelsOperation) Invoke(call sdkgo.Call, _ ListModelsInput) sdkgo.QueryAttempt[ModelsList] {
	branches := queryBranches{found: ListModelsBranchListed, rejected: ListModelsBranchProviderRejected, invalid: ListModelsBranchInvalidResponse, defect: ListModelsBranchDefect}
	return invokeJSONQuery(call, operation.client, "listModels", branches, http.MethodGet, "/v1/models", nil, nil, decodeModelsList)
}

func decodeModelsList(body []byte) (ModelsList, error) {
	var wire []struct {
		ModelID                     string `json:"model_id"`
		Name                        string `json:"name"`
		CanDoTextToSpeech           bool   `json:"can_do_text_to_speech"`
		MaximumTextLengthPerRequest int    `json:"maximum_text_length_per_request"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return ModelsList{}, err
	}
	if wire == nil {
		return ModelsList{}, fmt.Errorf("model catalog is missing")
	}
	models := ModelsList{Models: make([]ModelMetadata, 0, len(wire))}
	for _, model := range wire {
		if !voiceIDPattern.MatchString(model.ModelID) || model.MaximumTextLengthPerRequest < 0 {
			return ModelsList{}, fmt.Errorf("model identity or input limit is invalid")
		}
		models.Models = append(models.Models, ModelMetadata{ModelID: model.ModelID, Name: model.Name, CanDoTextToSpeech: model.CanDoTextToSpeech, MaximumTextLengthPerRequest: model.MaximumTextLengthPerRequest})
	}
	return models, nil
}

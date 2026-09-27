// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package llm

import (
	"errors"
	"regexp"
	"strings"
)

// ModelIDRule selects how a provider model ID is validated before any request.
// The zero value is invalid.
type ModelIDRule uint8

const (
	// ModelIDRuleBody is for providers that send the model in the JSON body.
	// After trimming surrounding whitespace, the ID must be 1 to 256 bytes of
	// printable ASCII from 0x21 through 0x7E. IDs are otherwise opaque and
	// case-sensitive, so fine-tune IDs such as "ft:base:org::id" pass unchanged.
	ModelIDRuleBody ModelIDRule = iota + 1
	// ModelIDRulePathSegment is for providers that put the model in a URL path
	// segment. After trimming whitespace and one leading "models/", the ID must
	// match ^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$, so it cannot change the path.
	ModelIDRulePathSegment
)

var pathSegmentModelIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

var (
	errBodyModelID = errors.New("model ID must be 1 to 256 printable ASCII characters without spaces")
	errPathModelID = errors.New(`model ID must start with a letter or digit and contain at most 128 letters, digits, ".", "_", or "-"`)
)

// String returns the rule's stable fixture name: "body" or "pathSegment".
func (rule ModelIDRule) String() string {
	switch rule {
	case ModelIDRuleBody:
		return "body"
	case ModelIDRulePathSegment:
		return "pathSegment"
	default:
		return "invalid"
	}
}

// ValidateModelID returns the canonical model ID the provider receives, or an
// error that never repeats the value. The same cases are pinned for the
// TypeScript picker in llmtest's model_id_cases.json fixture.
func (rule ModelIDRule) ValidateModelID(value string) (string, error) {
	model := strings.TrimSpace(value)
	switch rule {
	case ModelIDRuleBody:
		if len(model) < 1 || len(model) > 256 {
			return "", errBodyModelID
		}
		for index := 0; index < len(model); index++ {
			if model[index] < 0x21 || model[index] > 0x7E {
				return "", errBodyModelID
			}
		}
		return model, nil
	case ModelIDRulePathSegment:
		model = strings.TrimPrefix(model, "models/")
		if !pathSegmentModelIDPattern.MatchString(model) {
			return "", errPathModelID
		}
		return model, nil
	default:
		return "", errors.New("model ID rule is invalid")
	}
}

// ResolveModel applies generateText model precedence: the request's Model with
// surrounding whitespace trimmed when it is not blank, otherwise
// connectionModel, which the connector has already defaulted from its
// configuration. An empty result means no model is selected, which the
// pipeline reports as defect without a request.
func ResolveModel(requestModel string, connectionModel string) string {
	if model := strings.TrimSpace(requestModel); model != "" {
		return model
	}
	return connectionModel
}

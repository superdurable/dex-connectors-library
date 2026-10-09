// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package connectormock

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

type caseKind uint8

const (
	caseKindInvalid caseKind = iota
	caseKindBranch
	caseKindRetry
	caseKindUncertain
	caseKindPaged
)

// Case is one scripted provider outcome for an operation whose output type is
// OUT. Build it with Branch, Retry, Uncertain, or Paged; the zero Case is
// invalid, and a mock answers it with the defect branch.
//
// A Case is an immutable value and may be shared by several mocks and scripts.
type Case[OUT any] struct {
	kind       caseKind
	branch     sdkgo.BranchID
	value      OUT
	failure    *sdkgo.Failure
	retryAfter time.Duration
	pages      []OUT
	inputType  reflect.Type
	pageNumber func(input any) int
}

// Branch returns a terminal outcome that selects branch with value and an
// optional failure. The branch must be declared by the operation's definition;
// otherwise the SDK selects the defect branch, as it does for a real connector.
//
// An empty failure Provider or Operation is filled with the operation's
// connector and operation IDs when the mock answers.
func Branch[OUT any](branch sdkgo.BranchID, value OUT, failure *sdkgo.Failure) Case[OUT] {
	return Case[OUT]{kind: caseKindBranch, branch: branch, value: value, failure: failure}
}

// Retry returns an attempt that asks Dex to retry the Step after the optional
// provider delay. Zero after uses the Step's retry policy. A Retry is never
// remembered for an idempotency key, so the retried attempt takes the next
// scripted case.
//
// OUT cannot be inferred from the arguments, so call it as
// Retry[Output](failure, after) or use a generated mock package's typed
// constructor.
func Retry[OUT any](failure sdkgo.Failure, after time.Duration) Case[OUT] {
	return Case[OUT]{kind: caseKindRetry, failure: &failure, retryAfter: after}
}

// Uncertain returns a dispatched Mutation outcome that cannot be confirmed. The
// SDK selects the standard uncertain branch. It is valid only for a Mutation
// that declares that branch; a Query mock or a Mutation without the branch
// answers it with the defect branch.
func Uncertain[OUT any](value OUT, failure sdkgo.Failure) Case[OUT] {
	return Case[OUT]{kind: caseKindUncertain, branch: sdkgo.UncertainBranchID, value: value, failure: &failure}
}

// Paged returns one page of a multi-page listing on branch for every call.
// pageNumber reads the one-based page number that the operation input
// requests; the mock answers page n with pages[n-1]. A requested page outside
// the pages fails the test and selects the defect branch.
//
// A Paged case keeps answering while it is a When or Default case. In a
// Respond queue it answers one call, like any other case.
func Paged[IN, OUT any](branch sdkgo.BranchID, pageNumber func(IN) int, pages ...OUT) Case[OUT] {
	return Case[OUT]{
		kind: caseKindPaged, branch: branch, pages: pages, inputType: reflect.TypeFor[IN](),
		pageNumber: func(input any) int { return pageNumber(input.(IN)) },
	}
}

// CursorPageNumber returns a page-number function for Paged that follows a
// listing's own cursor. inputField and nextField are JSON field names: the
// operation input's cursor and each page's next cursor. An input whose cursor
// is absent, null, zero, or empty requests the first page; an input whose
// cursor equals page k's next cursor requests page k+1. When every page names
// its successor's one-based number, the listing is numbered and an input
// cursor n requests page n, so 1 also requests the first page. Any other
// cursor returns zero, which the mock reports as a page outside the listing.
//
// Generated mock packages use it for operations whose manifest declares
// pagination, so numbered pages and opaque page tokens work alike.
func CursorPageNumber[IN, OUT any](inputField, nextField string, pages []OUT) func(IN) int {
	nextCursors := make([]string, len(pages))
	for index, page := range pages {
		nextCursors[index] = jsonFieldValue(page, nextField)
	}
	isNumbered := true
	for index, nextCursor := range nextCursors[:max(len(nextCursors)-1, 0)] {
		isNumbered = isNumbered && nextCursor == strconv.Itoa(index+2)
	}
	return func(input IN) int {
		cursor := jsonFieldValue(input, inputField)
		if isEmptyJSONValue(cursor) {
			return 1
		}
		if pageNumber, err := strconv.Atoi(cursor); isNumbered && err == nil {
			return pageNumber
		}
		for index, nextCursor := range nextCursors {
			if !isEmptyJSONValue(nextCursor) && nextCursor == cursor {
				return index + 2
			}
		}
		return 0
	}
}

// DecodeOutput strictly decodes one JSON fixture into OUT. It rejects unknown
// fields and trailing data. When OUT or *OUT has a Validate() error method, the
// decoded value must also pass it, so a fixture obeys the connector's own
// output validation.
func DecodeOutput[OUT any](encoded string) (OUT, error) {
	var value OUT
	decoder := json.NewDecoder(strings.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		return value, fmt.Errorf("decode %T fixture: %w", value, err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return value, fmt.Errorf("decode %T fixture: trailing data after the value", value)
	}
	if validator, ok := any(value).(outputValidator); ok {
		if err := validator.Validate(); err != nil {
			return value, fmt.Errorf("validate %T fixture: %w", value, err)
		}
	} else if validator, ok := any(&value).(outputValidator); ok {
		if err := validator.Validate(); err != nil {
			return value, fmt.Errorf("validate %T fixture: %w", value, err)
		}
	}
	return value, nil
}

// MustDecodeOutput is DecodeOutput for fixtures that a generated test already
// verified. It panics when the fixture does not decode.
func MustDecodeOutput[OUT any](encoded string) OUT {
	value, err := DecodeOutput[OUT](encoded)
	if err != nil {
		panic(err)
	}
	return value
}

type outputValidator interface {
	Validate() error
}

// jsonFieldValue returns the compact JSON of one top-level field, or "" when
// the value is not a JSON object or lacks the field.
func jsonFieldValue(value any, field string) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &fields); err != nil {
		return ""
	}
	raw, found := fields[field]
	if !found {
		return ""
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, raw); err != nil {
		return ""
	}
	return compact.String()
}

func isEmptyJSONValue(value string) bool {
	switch value {
	case "", "null", "0", `""`:
		return true
	default:
		return false
	}
}

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package stripe

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

// flexibleObjectID accepts Stripe fields that may be an ID, null, or an expanded object.
type flexibleObjectID string

// UnmarshalJSON accepts a provider object ID, null, or an expanded object.
func (identifier *flexibleObjectID) UnmarshalJSON(contents []byte) error {
	if bytes.Equal(contents, []byte("null")) {
		*identifier = ""
		return nil
	}
	var value string
	if err := json.Unmarshal(contents, &value); err == nil {
		*identifier = flexibleObjectID(value)
		return nil
	}
	var expanded struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(contents, &expanded); err != nil {
		return err
	}
	*identifier = flexibleObjectID(expanded.ID)
	return nil
}

func jsonUnmarshal(contents []byte, output any) error {
	decoder := json.NewDecoder(bytes.NewReader(contents))
	if err := decoder.Decode(output); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return fmt.Errorf("JSON contains trailing data")
	}
	return nil
}

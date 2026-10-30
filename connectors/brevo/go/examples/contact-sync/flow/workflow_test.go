// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package contactsync

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/brevo/go"
)

func TestMapToCreateOrUpdateContactInput(t *testing.T) {
	require.Equal(t, brevo.CreateOrUpdateContactInput{Email: "member@meridian.example", Attributes: map[string]string{"FNAME": "Meridian"}}, MapToCreateOrUpdateContactInput(Request{Email: "member@meridian.example", FirstName: "Meridian"}))
}

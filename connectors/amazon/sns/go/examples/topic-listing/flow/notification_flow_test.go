// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package topiclisting

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/amazon/sns/go"
)

func TestNotificationBindingShowsGuidedTopicARNInput(t *testing.T) {
	bindings := (&NotificationFlow{}).GetConnectorTriggerBindings()
	require.Len(t, bindings, 1)
	binding := bindings[0]
	require.NoError(t, binding.Validate())
	require.NotNil(t, binding.ConfigurationUI)
	require.Len(t, binding.ConfigurationUI.Units, 1)
	unit := binding.ConfigurationUI.Units[0]
	require.Equal(t, sns.UIUnitTopicARNInput, unit.UnitID)
	require.True(t, unit.Required)
	require.Equal(t, sns.UITopicARNInputPortTopicARN, unit.Bindings[0].Port)
	require.Equal(t, "/topicARN", unit.Bindings[0].JSONPointer)
	for _, guidance := range []string{"https://console.aws.amazon.com/sns", "Topics", "arn:aws:sns:", "non-secret", "blank", "Region"} {
		require.True(t, strings.Contains(unit.Description, guidance), guidance)
	}
}

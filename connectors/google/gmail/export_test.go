// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package gmail

import "github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"

// NewDurableMessageTriggerRunnerForTest builds the NewProjectMessageTriggerRunner runner over test inbox storage.
func NewDurableMessageTriggerRunnerForTest(
	connection Connection,
	configuration projectconfig.Configuration,
	openInbox func(projectconfig.TriggerInboxKey) (*projectconfig.TriggerInbox, error),
	config ProjectMessageTriggerRunnerConfig,
) (*MessageTriggerRunner, error) {
	return newDurableMessageTriggerRunner(connection, configuration, openInbox, config)
}

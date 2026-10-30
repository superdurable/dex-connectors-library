// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package intercom

// NewConversationEventEndpointRunnerForTest builds the project runner from in-memory configuration; its last
// argument replaces the durable inboxes.
var NewConversationEventEndpointRunnerForTest = newConversationEventEndpointRunner

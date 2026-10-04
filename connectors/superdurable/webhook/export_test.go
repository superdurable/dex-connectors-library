// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package webhook

// NewRequestReceivedEndpointRunnerForTest builds the project runner from in-memory configuration; its last
// argument replaces the durable inboxes.
var NewRequestReceivedEndpointRunnerForTest = newRequestReceivedEndpointRunner

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package stripe

// NewCheckoutSessionWebhookRuntimeForTest builds the project runtime from in-memory configuration; its last
// argument replaces the durable inboxes.
var NewCheckoutSessionWebhookRuntimeForTest = newCheckoutSessionWebhookRuntime

# Telegram Bot TypeScript connector

This package implements `sendMessage` and `getChat` for Dex TypeScript Flows.
The send operation claims a Dex single-dispatch checkpoint before contacting
Telegram. A definite 429 or 503 permits a later resend; an ambiguous outcome
selects `uncertain`. The Bot token stays in credentials and the API request
path, outside Flow state and results. Both operations replay the Go provider
conformance scenarios. Runnable Flows are under `examples/`.

# PPIO connector

This connector implements the read-only [model list API](https://ppio.com/docs/models/reference-llm-list-models). It sends a PPIO API key as a Bearer header to the fixed `https://api.ppio.com/openai/v1/models` endpoint and returns the documented model ID, display name, status, context size, supported endpoints, Unix creation time, and owner. It never places the key in Flow input, output, receipts, Attributes, or logs. An empty list is valid.

Create an API key from [API key management](https://ppio.com/settings/key-management). Enter the full key in Dex Web Connections. Rate limits and temporary service errors retry; invalid keys select `providerRejected`.

The runnable [list-models example](go/examples/list-models/README.md) demonstrates the generated operation-specific Step and static `ppio` connection. Real PPIO authorization remains to be tested with a user-controlled Dex Web Connection.

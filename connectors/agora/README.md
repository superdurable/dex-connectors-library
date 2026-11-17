# Agora Conversational AI connector

The Go `listRunningAgents` Query calls Agora's [documented agent list endpoint](https://docs.agora.io/en/api-reference/api-ref/conversational-ai/list). It reads one page of currently running Conversational AI agents from the preceding two hours, using the provider's default 20-item limit. The output contains each agent ID, status, start timestamp in Unix seconds, page count, total, and cursor. This is an account observation and does not start or stop agents.

## Configure Dex Web Connections

At [Agora Console](https://console.agora.io), open **Projects**, select a project, then open its **Credentials** panel. Copy the 32-character App ID to `appId`. From the account menu at the bottom of the sidebar, open **RESTful API Keys**, create a key and download it once. Enter the downloaded **Key** as `customerId` and **Secret** as `customerSecret`. The latter is a secret Dex credential. Agora [requires Basic authentication](https://docs.agora.io/en/api-reference/api-ref/rtc/authentication) for these credentials. Leave `baseUrl`, `timeout`, and `maxResponseBytes` blank for their displayed defaults.

A missing or invalid local credential selects optional `defect`. Definite HTTP 4xx rejection selects optional `providerRejected`. HTTP 429, HTTP 5xx and transport failures retry. Malformed or oversized JSON selects optional `invalidResponse`. The example leaves optional branches unwired, so these outcomes fail the Flow.

The runnable example is in [`go/examples/agent-list`](go/examples/agent-list). Tests exercise a local HTTP provider and a real Dex stack with the generated mock. Live Agora authorization and current agent discovery remain unverified without a user-controlled project.
# Python

The [Python package](python/README.md) implements `listRunningAgents` with a
runnable Dex Flow and Go-recorded provider conformance scenarios.


The [TypeScript package](typescript/README.md) implements the same `listRunningAgents` Query with Go-recorded conformance scenarios and a runnable Dex Flow.

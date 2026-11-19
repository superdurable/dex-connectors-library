# Agora connector for Python

Install `dex-connector-agora==0.1.0`. The package implements `listRunningAgents`
with the same four branches and retry classification as the Go connector.

The Query reads one page of running agents from Agora's
[`/api/conversational-ai-agent/v2`](https://docs.agora.io/en/api-reference/api-ref/conversational-ai)
REST API. It returns at most 20 agents with the provider's total and next
cursor. `providerRejected`, `invalidResponse`, and `defect` are optional branches;
an unwired optional outcome fails the Flow. HTTP 408, 429, 5xx, and transport
failures retry.

Create a Dex Web connection named `agora` using the
[authorization guide](../README.md#configure-dex-web-connections). App ID, Customer ID, and
Customer Secret are resolved for every attempt. Credentials never enter the
Flow result.

[`examples/agent_list`](examples/agent_list/flow.py) is the runnable consuming
Flow. After configuring `agora` in Dex Web, run `uv run python -m
examples.agent_list.worker` while `dexcli dev` is active. The example's static
connection name lets Dex Web show its configuration.

Run `uv run pytest tests` for Go-recorded provider conformance and example
contract tests. Live Agora authorization and provider response have not been
verified with an account.

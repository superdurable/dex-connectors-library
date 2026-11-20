# TiankeChat Python connector

This package provides the `listKnowledgeEntries` Query for TiankeChat knowledge metadata. Configure the static `tiankechat` connection in Dex Web; the App Secret remains in its secret credential. See [the runnable Worker and Flow](examples/list_knowledge_entries/README.md) and [Go-recorded scenarios](../../../conformance/connectors/tiankechat/scenarios.json). Real TiankeChat authorization has not been verified.

Run `uv run pytest -q` for conformance and `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 uv run pytest examples/list_knowledge_entries/integration_test.py` against a running Dex stack.

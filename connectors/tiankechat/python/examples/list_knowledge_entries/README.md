# TiankeChat listKnowledgeEntries example

Run `uv run python -m examples.list_knowledge_entries.worker` after configuring the static `tiankechat` connection in Dex Web. Run `dexcli visualize --open=false` on `examples/list_knowledge_entries/flow.py` to inspect the Flow graph. With a local Dex stack and `DEX_FLOW_SERVICE_ADDRESS`, run `uv run pytest examples/list_knowledge_entries/integration_test.py`. The integration test uses synthetic provider responses; live TiankeChat authorization remains unverified.

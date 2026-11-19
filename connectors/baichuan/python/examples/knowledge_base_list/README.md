# Baichuan knowledge-base list Flow

Configure the static `baichuan` connection in Dex Web Connections with an approved Baichuan API key. Run `python -m examples.knowledge_base_list.worker` from this package's Python project after starting `dexcli dev`, then start `BaichuanKnowledgeBaseList` with `{}`. The Flow persists the returned list as an Attribute. The [Flow source](flow.py) is the executable API example.

The integration test uses an inert credential and local provider transport; real provider authorization remains unverified.

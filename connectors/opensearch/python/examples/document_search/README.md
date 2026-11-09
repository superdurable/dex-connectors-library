# Document search Flow

`DocumentSearchFlow` uses the static Dex Web `opensearch` connection and `new_search_documents_step`. Register it with `examples.worker`, then start a Flow with `SearchDocumentsInput`. The result is persisted as `SearchPage`.

Generate strict FDG 2.0: `dexcli visualize examples/document_search/flow.py --schema-version 2.0 --json --out /tmp/opensearch-python-document_search-fdg --lint app`.

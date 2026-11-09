# Document index Flow

`DocumentIndexFlow` uses the static Dex Web `elasticsearch` connection and `new_index_document_step`. Register it with `examples.worker`, then start a Flow with `IndexDocumentInput`. The result is persisted as `IndexedDocument`.

Generate strict FDG 2.0: `dexcli visualize examples/document_index/flow.py --schema-version 2.0 --json --out /tmp/elasticsearch-python-document_index-fdg --lint app`.

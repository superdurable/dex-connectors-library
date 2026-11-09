# Document delete Flow

`DocumentDeleteFlow` uses the static Dex Web `elasticsearch` connection and `new_delete_document_step`. Register it with `examples.worker`, then start a Flow with `DeleteDocumentInput`. The result is persisted as `DeletedDocument`.

Generate strict FDG 2.0: `dexcli visualize examples/document_delete/flow.py --schema-version 2.0 --json --out /tmp/elasticsearch-python-document_delete-fdg --lint app`.

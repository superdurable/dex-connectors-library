# JINGdigital Python connector

This package provides the read-only `listTags` Query for ordinary marketing tag metadata. Create a JINGdigital application and enable tag list access as described in the [company guide](../README.md). The operation-specific `new_list_tags_step` returns tag IDs, names, levels, total count, and next page. The example wires the happy `listed` branch; `providerRejected`, `invalidResponse`, and `defect` are optional.

Run `uv sync --locked` and `uv run pytest` in this directory. The [tag directory Flow](examples/tag_directory/) includes a Worker and a real Dex retry/persistence integration test with synthetic provider responses. Live JINGdigital authorization has not been verified.

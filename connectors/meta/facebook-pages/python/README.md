# Facebook Pages Python connector

Read one Page feed with `listPosts`, inspect a post with `getPost`, or publish plain text with `publishTextPost`. The read results include only post ID, message, creation time, and permalink; comments, reactions, and audience data are excluded. Publishing uses a durable single-dispatch checkpoint and selects `uncertain` when a sent request cannot be confirmed. Inspect the Page before a manual retry.

The package uses the static `facebook-pages` connection and a Page access token. See the [connector README](../README.md) for Meta setup, the [feed Flow](examples/page_posts/README.md) and [publishing Flow](examples/post_publish/README.md) for Dex usage. Go-recorded provider scenarios are replayed by `tests/test_conformance.py`.

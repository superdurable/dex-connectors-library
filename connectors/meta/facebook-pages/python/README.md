# Facebook Pages Python connector

Read one Page feed with `listPosts` or inspect a post with `getPost`. The result includes only post ID, message, creation time, and permalink; comments, reactions, and audience data are excluded.

The package uses the static `facebook-pages` connection and a Page access token. See the [connector README](../README.md) for Meta setup and the [runnable Flow](examples/page_posts/README.md) for Dex usage. Go-recorded provider scenarios are replayed by `tests/test_conformance.py`.

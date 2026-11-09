# WordPress Python connector

This package reads published posts with `getPost` and `listPosts`, and creates drafts or published posts with `createPost`. Post creation uses a Dex single-dispatch checkpoint because WordPress has no create-post idempotency key. A definite 429 or 503 may retry; an ambiguous result selects `uncertain` and requires inspection in WordPress Posts before another creation attempt.

Set up the static `wordpress` connection in Dex Web with the site's HTTPS Site Address, WordPress username, and an application password created under **Users > Profile > Application Passwords**. The application password remains in Dex credentials. Post title and content are untrusted HTML when read from WordPress.

The package replays the Go-recorded provider scenarios. Runnable Flow examples are in `examples/`; live WordPress authorization and delivery remain unverified until a user-controlled connection is available.

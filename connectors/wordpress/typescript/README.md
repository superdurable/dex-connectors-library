# WordPress TypeScript connector

This package implements `createPost`, `getPost`, and `listPosts` for Dex TypeScript Flows. Post creation uses a Dex single-dispatch checkpoint because WordPress has no create-post idempotency key. A definite HTTP 429 or 503 can retry; an ambiguous outcome selects `uncertain`. Inspect WordPress Posts before manually creating another post.

Configure the named `wordpress` Dex Web connection with the HTTPS Site Address, WordPress username, and an application password from **Users > Profile > Application Passwords**. Keep the password in Dex credentials. Published post title and content contain untrusted HTML and must be sanitized before display.

The package replays all Go-recorded provider scenarios. Runnable Flow examples are under `examples/provider-operations/`. Live WordPress authorization remains unverified without a user-controlled connection.

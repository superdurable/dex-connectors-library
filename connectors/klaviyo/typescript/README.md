# Klaviyo connector for TypeScript

Install with `npm install @superdurable/connector-klaviyo@0.1.0`. This package implements `getProfile` and `createEvent` with generated operation-specific Step factories and the Go connector's branches.

| Operation | Factory | Happy branch | Optional branches |
| --- | --- | --- | --- |
| `getProfile` | `newGetProfileStep` | `found` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |
| `createEvent` | `newCreateEventStep` | `accepted` | `providerRejected`, `defect` |

`createEvent` hashes Dex's stable call ID into the Klaviyo `unique_id`; retries use the same ID. HTTP 202 confirms acceptance for asynchronous processing. Both operations retry transport failures, 408, 409, 429, and 5xx. Missing or invalid credentials select `defect`; 401 and 403 select `providerRejected`.

Create the private API key through the [connector authorization guide](../README.md#authorization). The key is resolved on each attempt and sent only in the `Authorization` header.

The [runnable profile event example](examples/profile-event/README.md) binds the static `klaviyo-profile-events` connection. Run `npm run check` for package tests, type checking, and lint; run `npm run test:integration` against a local Dex CLI dev server. The Go-recorded provider scenarios replay through this package's conformance test.

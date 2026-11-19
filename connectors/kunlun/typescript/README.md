# Mureka TypeScript connector

`@superdurable/connector-kunlun` reads the status of an existing Mureka song-generation task through the read-only `getTaskStatus` Dex Step. It returns the task ID, state, and model, omitting lyrics and media URLs.

The [runnable Flow and Worker](examples/task-status/) use the static `kunlun` connection. In Dex Web Connections, create a Mureka API key at [Mureka Platform > API Keys](https://platform.mureka.cn/) and enter its opaque value without the `Bearer` prefix. Supply an existing song task ID as `taskId`. The endpoint, timeout, and response limit have manifest defaults.

The checked-in conformance scenarios replay Go-recorded requests and outcomes. The integration test exercises a real Dex Worker and Client with a local provider that returns 503 before succeeding. Real Mureka authorization and song-task status remain unverified without a user-controlled key and existing task.

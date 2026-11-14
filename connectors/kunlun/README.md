# Kunlun Tech Mureka connector

This connector implements the read-only [Mureka song-task query](https://platform.mureka.cn/docs/api/operations/get-v1-song-query-%7Btask_id%7D.html). It sends an API key as `Authorization: Bearer <key>` to `GET https://api.mureka.cn/v1/song/query/{task_id}`. The ID comes from an existing song-generation response. The connector does not create a task or spend credits.

Only the task ID, status, and model are returned. Mureka can also return generated audio URLs, lyrics, and failure details; these are omitted from durable output, receipts, Attributes, and logs. A failed song is a valid observed status. A missing task selects `notFound`; every HTTP 429, including account quota exhaustion, and temporary service errors retry and honor `Retry-After`. Error bodies are not logged or persisted.

Sign in at the [Mureka platform](https://platform.mureka.cn/) and open **API Keys** in the navigation. Create a key and enter its full value in Dex Web Connections without the `Bearer` prefix. The runnable [get-task-status example](go/examples/get-task-status/README.md) demonstrates the operation-specific Step and static `kunlun` connection. Real Mureka authorization has not been tested.

The [official quickstart](https://platform.mureka.cn/docs/cn/quickstart.html) documents API keys and Bearer authentication; the [error guide](https://platform.mureka.cn/docs/cn/error-codes.html) documents HTTP status handling.

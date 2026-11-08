# MeisterTask connector

The Go, Python, and TypeScript `getTask` Query reads a task's numeric ID, name, and status code. The Result excludes notes, assignee, and other task details.

## Connect

At the [MindMeister API page](https://www.mindmeister.com/api), add a personal access token with `userinfo.profile`, `userinfo.email`, and `meistertask` scopes. Save the secret token as `api_token` in Dex Web Connections. See [MeisterTask authentication](https://developers.meistertask.com/reference/authentication) and [personal access token setup](https://developers.mindmeister.com/docs/personal-access-token). The token owner must be able to view the task. MindMeister account activation may require a team administrator.

Leave `baseUrl`, `timeout`, and `maxResponseBytes` blank for their displayed defaults. Find the positive numeric task ID through [List tasks](https://developers.meistertask.com/reference/get-tasks) and pass its decimal form as `taskId`. The operation calls [Get a task](https://developers.meistertask.com/reference/get-task).

## Examples and verification

Run the [Go](go/examples/task-inspection/README.md), [Python](python/examples/task_inspection/README.md), or [TypeScript](typescript/examples/task-inspection/README.md) example with the static `meistertask` connection. Go-recorded conformance checks requests, branches, retries, and outputs. Real Dex integration tests use fixture providers. Live MeisterTask authorization and task retrieval remain unverified.

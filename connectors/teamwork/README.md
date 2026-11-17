# Teamwork.com connector

The Go connector provides `listProjects`, `getProject`, `createProject`, `updateProject`, `listTasks`, `getTask`, `createTask`, `updateTask`, and `deleteTask`. Python and TypeScript continue to provide `getTask`. Project descriptions, client details, task descriptions, and assignee details stay out of Dex Results.

## Connect

Follow the [Teamwork.com API guide](https://apidocs.teamwork.com/guides/teamwork/getting-started-with-the-teamwork-com-api): on your Teamwork.com site, open **avatar > Edit my details > API & Mobile > Show your token**. Copy the opaque Key to Dex Web `api_token`. The connector uses it as a Basic Auth username with an empty password. The key inherits the site user's permissions.

Set required `baseUrl` to the site's HTTPS root shown in the browser before `/app`; US, EU, and custom domains differ. `timeout` is a per-request Go duration and `maxResponseBytes` is a byte limit; blank uses their displayed defaults. Pass the numeric task ID as a decimal `taskId` string. The implementations accept IDs through 9007199254740991 for exact TypeScript JSON decoding. See [Get a specific task](https://apidocs.teamwork.com/docs/teamwork/v3/tasks/get-projects-api-v3-tasks-task-id-json).

`createTask` uses the [Teamwork task-create endpoint](https://apidocs.teamwork.com/docs/teamwork/v3/tasks/post-projects-api-v3-tasklists-tasklist-id-tasks-json), sends at most three possibly effective requests, and selects `uncertain` if Teamwork does not confirm the outcome; a task title is not a unique read-back marker. List, get, absolute title replacement, and delete are safe to repeat. [Delete](https://apidocs.teamwork.com/docs/teamwork/v3/tasks/delete-projects-api-v3-tasks-task-id-json) treats a definite 404 as already absent and also removes subtasks. `listTasks` returns one page of task summaries and accepts `page` and `pageSize` (1 through 500; blank or zero uses provider defaults).

Project reads use Teamwork's [V3 Projects API](https://apidocs.teamwork.com/docs/teamwork/v3/projects/get-projects-api-v3-projects-json). Project creation and absolute name replacement use the documented V1 [create](https://apidocs.teamwork.com/docs/teamwork/v1/projects/post-projects-json) and [update](https://apidocs.teamwork.com/docs/teamwork/v1/projects/put-projects-id-json) endpoints. `createProject` is limited to three possibly effective sends and selects `uncertain` when no project ID is confirmed; a user-chosen name is not a unique read-back marker. Teamwork's documented Projects API does not expose project deletion, so this connector does not offer it. `listProjects` accepts `page` and `pageSize` (1 through 500; zero selects the provider defaults).

## Examples and verification

Run the [Go](go/examples/task-inspection/README.md), [Python](python/examples/task_inspection/README.md), or [TypeScript](typescript/examples/task-inspection/README.md) example with the static `teamwork` connection. Go-recorded conformance checks requests, branches, retries, and outputs. Real Dex integration tests use fixture providers. Live Teamwork.com authorization and task operations remain unverified.

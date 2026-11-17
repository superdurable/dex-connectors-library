# MeisterTask connector

The Go, Python, and TypeScript `getTask` Query reads a task's numeric ID, name, and status code. Go also provides `listTasks`, `updateTask`, `createTask`, `listProjects`, `getProject`, `updateProject`, `createProject`, `listSections`, `getSection`, `updateSection`, and `createSection`. Results contain only IDs, names, status codes, and section parent project IDs; notes, membership, and other details stay out of Flow state.

`listTasks` requests one page of available tasks, optionally filtered by status. `updateTask` sets an absolute name, notes, or status and can repeat safely. `createTask` posts a new task in a section with a bounded send count; MeisterTask permits duplicate task names, so an ambiguous create cannot be confirmed through a name lookup. Inspect the section if the `uncertain` branch is selected.

`listProjects` requests one page with optional `active`, `archived`, or `all` status. `getProject` reads a project by positive decimal `projectId`. `updateProject` sets an absolute name, notes, or status (`1` active, `4` trashed, `5` archived) and repeats safely. `createProject` is a bounded create; project names are not unique, so inspect the project list if its `uncertain` branch is selected.

`listSections` requests one page with optional `active`, `trashed`, or `all` status. `getSection` reads a section by positive decimal `sectionId`. `updateSection` sets an absolute name or status (`1` active, `2` trashed). `createSection` requires a positive parent `projectId` and a nonempty name; section names are not unique, so inspect the section list if its bounded create selects `uncertain`.

## Connect

At the [MindMeister API page](https://www.mindmeister.com/api), add a personal access token with `userinfo.profile`, `userinfo.email`, and `meistertask` scopes. Save the secret token as `api_token` in Dex Web Connections. See [MeisterTask authentication](https://developers.meistertask.com/reference/authentication) and [personal access token setup](https://developers.mindmeister.com/docs/personal-access-token). The token owner must be able to view the task. MindMeister account activation may require a team administrator.

Leave `baseUrl`, `timeout`, and `maxResponseBytes` blank for their displayed defaults. Find the positive numeric task ID through [List tasks](https://developers.meistertask.com/reference/get-tasks) and pass its decimal form as `taskId`. The operation calls [Get a task](https://developers.meistertask.com/reference/get-task). Go's task writes use the documented [Create a task](https://developers.meistertask.com/reference/post-task) and [Update a task](https://developers.meistertask.com/reference/put-task) endpoints; find a positive section ID on the project board before creation.

The [project API reference](https://developers.meistertask.com/reference/project) documents project list, create, get, and update calls. Find `projectId` in [List projects](https://developers.meistertask.com/reference/get-projects) or the project URL; blank is invalid. `page` and `items` are positive integers, with zero selecting the first page or provider page size. Blank `status` uses active projects. `name` must be nonempty for creation; blank `notes` omits project detail.

The [section API reference](https://developers.meistertask.com/reference/section) documents section list, create, get, and update calls. Find `sectionId` in [List sections](https://developers.meistertask.com/reference/get-sections) or the project board. A section create uses a parent project ID from the project list; blank IDs or names are invalid. Section page and item values use the same units as project listing, and blank status uses active sections.

## Examples and verification

Run the [Go](go/examples/task-inspection/README.md), [Python](python/examples/task_inspection/README.md), or [TypeScript](typescript/examples/task-inspection/README.md) example with the static `meistertask` connection. The Go example also has task and project listing Flows. Go-recorded conformance checks requests, branches, retries, and outputs. Real Dex integration tests use fixture providers. Live MeisterTask authorization and provider calls remain unverified.

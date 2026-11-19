# Basecamp list Flow

This runnable Go Flow lists one authorized account page through the `basecamp` Dex Web connection. Start with `ListAccountsInput{}` for the default page size or supply a zero-based `Offset` and a `Limit` from 1 through 100. The result is persisted in the Flow Attribute and exposed through Dex Web summary and display RPCs.

From the connector's `go/` directory run `GOWORK=off go test ./...`. Generate and lint FDG 2.0 with `dexcli visualize go/examples/accounts/flow/workflow.go --schema-version 2.0 --lint app --json --out /tmp/basecamp-fdg --open=false` from the connector directory. With `dexcli dev` running, set `DEX_FLOW_SERVICE_ADDRESS` and run `GOWORK=off go test -tags=integration ./examples/accounts/flow -run TestAccountsUsesDefaultMockWithRealDex` to exercise the checked-in mock through a real Worker and Client.

With the Basecamp OAuth connection saved in Dex Web, run `GOWORK=off go run ./examples/accounts` to start the Worker. Live Basecamp authorization has not been exercised here.
# Project listing

The same worker registers `BasecampProjectsList`. Start it with a `ListProjectsInput` containing the numeric `accountId` returned by `BasecampAccountsList`. Its `CallListProjects` Step reads one provider page and completes with a `ProjectsPage`. The static Dex Web connection remains `basecamp`.

# Project creation

The worker also registers `BasecampProjectCreate`. Start it with a `CreateProjectInput` containing the account ID, a non-blank project name, and optional description. The `CallCreateProject` Step creates one project with a bounded resend count and completes with its ID and name. Run `dexcli visualize go/examples/accounts/flow/project_create.go --schema-version 2.0 --lint app --json --out /tmp/basecamp-project-create-fdg --open=false` from the connector directory, then run `GOWORK=off go test -tags=integration ./examples/accounts/flow -run '^TestProjectCreateUsesDefaultMockWithRealDex$'` with `DEX_FLOW_SERVICE_ADDRESS` pointed to local Dex.

# To-do creation

The worker registers `BasecampTodoCreate`. Start it with a `CreateTodoInput` containing the account and to-do list IDs, a non-blank title, and optional description, dates, assignees, and subscribers. `CallCreateTodo` uses a bounded send count and completes with the created to-do. Run `dexcli visualize go/examples/accounts/flow/todo_create.go --schema-version 2.0 --lint app --json --out /tmp/basecamp-todo-create-fdg --open=false` from the connector directory, then run `GOWORK=off go test -tags=integration ./examples/accounts/flow -run '^TestTodoCreateUsesDefaultMockWithRealDex$'` with `DEX_FLOW_SERVICE_ADDRESS` pointed to local Dex.

# To-do list creation

The worker registers `BasecampTodoListCreate`. Start it with a `CreateTodoListInput` containing the account ID, to-do set ID from the project dock, non-blank list name, and optional rich text description. `CallCreateTodoList` uses a bounded send count and completes with the created list. Run `dexcli visualize go/examples/accounts/flow/todo_list_create.go --schema-version 2.0 --lint app --json --out /tmp/basecamp-todo-list-create-fdg --open=false` from the connector directory, then run `GOWORK=off go test -tags=integration ./examples/accounts/flow -run '^TestTodoListCreateUsesDefaultMockWithRealDex$'` with `DEX_FLOW_SERVICE_ADDRESS` pointed to local Dex.

# Person enrollment

The worker registers `BasecampPersonEnrollment`. Start it with an `EnrollPersonInput` containing the account ID, display name, email address, optional project IDs, and optional invitation notification choice. `CallEnrollPerson` uses a bounded send count and completes with the enrolled person's ID and whether the provider created or reused the account person. Run `dexcli visualize go/examples/accounts/flow/person_enrollment.go --schema-version 2.0 --lint app --json --out /tmp/basecamp-person-enrollment-fdg --open=false` from the connector directory, then run `GOWORK=off go test -tags=integration ./examples/accounts/flow -run '^TestPersonEnrollmentUsesDefaultMockWithRealDex$'` with `DEX_FLOW_SERVICE_ADDRESS` pointed to local Dex.

# Message creation

The worker registers `BasecampMessageCreate`. Start it with a `CreateMessageInput` containing the account and message board IDs, non-blank subject, optional rich text content, publication choice, and subscriber IDs. The default saves a draft; `Publish: true` posts immediately. `CallCreateMessage` bounds repeated sends and completes with the created message. Run `dexcli visualize go/examples/accounts/flow/message_create.go --schema-version 2.0 --lint app --json --out /tmp/basecamp-message-create-fdg --open=false` from the connector directory, then run `GOWORK=off go test -tags=integration ./examples/accounts/flow -run '^TestMessageCreateUsesDefaultMockWithRealDex$'` with `DEX_FLOW_SERVICE_ADDRESS` pointed to local Dex.

# Schedule creation

The worker registers `BasecampScheduleEntryCreate`. Start it with a `CreateScheduleEntryInput` containing the account ID, schedule ID from the project dock, non-blank summary, and an interval. Use RFC3339 timestamps for timed entries or YYYY-MM-DD dates with `AllDay: true`. Optional description, participant IDs, and `Draft: true` control content and publication. `CallCreateScheduleEntry` bounds repeated sends and completes with the created entry. Run `dexcli visualize go/examples/accounts/flow/schedule_entry_create.go --schema-version 2.0 --lint app --json --out /tmp/basecamp-schedule-create-fdg --open=false` from the connector directory, then run `GOWORK=off go test -tags=integration ./examples/accounts/flow -run '^TestScheduleEntryCreateUsesDefaultMockWithRealDex$'` with `DEX_FLOW_SERVICE_ADDRESS` pointed to local Dex.

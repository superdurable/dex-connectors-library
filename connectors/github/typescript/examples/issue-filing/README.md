# GitHub issue filing Flow

This Flow files one issue, such as a bug report, unless an open issue with the same title is already tracked. It uses the static Dex Web connection name `github-issue-filing`, which must use the **GitHub OAuth, repository issues** (`github-oauth-repository`) method.

1. `ListOpenGitHubIssues` reads open issues that carry every requested label, 100 per page, and follows `nextPage` for at most 3 pages.
2. `RecordOpenGitHubIssues` completes the Flow with the existing issue when a title matches, ignoring case and surrounding whitespace.
3. `CreateGitHubIssue` creates the issue once. A retry first reads the saved issue number, or lists the newest issues the connection's account created and matches the hidden marker in their bodies, and sends again only while the send cap allows.

Only the `listed` and `created` branches are wired; any other branch, such as `insufficientScope` on a `github-oauth` connection or `uncertain`, fails the Flow. The `github-issue-filing-report` Attribute records the request, the pages read, and the existing or created issue.

## Run

1. Start at [GitHub Developer settings](https://github.com/settings/developers), open **OAuth Apps**, register the exact Dex redirect URI, and copy the client ID and secret. In Dex Web **Connections**, select `github / github-issue-filing`, choose the **GitHub OAuth, repository issues** method, enter the client ID and secret, and authorize. Dex requests `repo` and `offline_access`; `repo` reads and writes code, issues, and pull requests in every repository the account can reach, so authorize with an account limited to the repositories you file issues in.
2. Start a Dex development server and set `DEX_FLOW_SERVICE_ADDRESS` to its gRPC address.
3. In this package, run `npm ci`, then `npm run example:issue-filing` for the Worker. In another terminal run `npm run example:issue-filing:start -- examples/issue-filing/input.example.json`. Running the start command twice reports `alreadyTracked` the second time. Close the test issue on GitHub afterwards.

The input contains `owner`, `repository`, `title`, an optional Markdown `body`, and optional `labels`. No token belongs in the input or source files.

`npm run test:integration` runs the same Flow through a real Dex Worker and Client with a local GitHub stand-in. It covers a create after two open pages, an already tracked title, a create GitHub stored but answered with `502` whose Step retry reads it back instead of sending again, a create GitHub never stored that is sent once more after read-back, and a failure without any create on a `github-oauth` connection. Live GitHub authorization and API behavior have not been verified by that test.

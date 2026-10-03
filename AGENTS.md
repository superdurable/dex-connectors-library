# Dex Connectors Library — Codex Instructions

Open-source Dex-native connector SDKs, provider connectors, manifests, code
generation, React primitives, directory UI, and examples.

After a contributor enters this checkout, this file, `docs/`, and the owning
module README are the sole connector-authoring authority. The installed Dex
Connector Contributor skill may locate and bootstrap the repository, but it
does not own implementation, acceptance, or release rules.

## Plan Mode

Every implementation plan includes Tests, Documentation, and UI/UX. Use
`N/A: <one-line reason>` only when a section genuinely does not apply.

### Tests

- List specific scenarios and why each needs unit, integration, or E2E coverage.
- Prefer real Dex integration tests for Step, retry, Trigger delivery, Stream,
  Attribute, RPC, and transition behavior.
- Do not add a unit test when the behavior is only meaningful across a real Dex
  Worker, Client, persistence, or retry boundary.

### Documentation

- Update the owning module README or `docs/` contract page when public behavior
  or authoring guidance changes.
- Application snippets must come from runnable files under `examples/` or a
  connector's `examples/` tree. Never invent an API for documentation.

### UI/UX

- State whether React primitives, Connector Studio UI, or the directory site is
  affected. Do not invent UI work for changes without a visible surface.

## Repository Boundaries

- Keep the Go SDK in `sdkgo` and every connector in its own Go module.
- Group one company's connectors below one directory. The first directory under
  `connectors/` is the company, must match `metadata.company`, and must contain
  `logo.svg`.
- Provider operation calls made for a Flow belong in Dex Step `Execute`; RPC
  handlers must not call providers. Trigger sources may poll or subscribe to
  provider events before routing them to a Flow, and setup UI provider commands
  may perform their declared read-only setup operations.
- Preserve stable Flow, Step, Attribute, Stream, connector, operation, branch,
  Trigger, binding, and RPC identities.
- Register every Attribute and Stream in the consuming Flow persistence schema.
- Keep secrets out of Flow input, Attributes, Results, receipts, Streams, logs,
  generated values, UI artifacts, catalogs, and release artifacts.

## Connector Authoring

- `connector.yaml` is the source for company, release version, generated Config,
  Credentials, definitions, branch fields, operation-specific Step factories,
  Trigger factories, UI metadata, and defaults.
- Normal application APIs use operation-specific factories such as
  `openai.NewCreateResponseStep`. Generic `sdkgo.NewQueryStep` and
  `sdkgo.NewMutationStep` are advanced escape hatches.
- Every connector has its own `go.mod`, README, manifest, generated code, and
  provider tests.
- Every connector has at least one runnable Flow under `examples/` with a
  Worker entrypoint, strict FDG 2.0 generation instructions, deterministic
  tests, and the connector's static `ConnectionName`.
- Register every connector directory in the sorted root `catalog.yaml` list.
  Paths may have any depth below `connectors/`.
- The catalog is the only shared file changed when adding a connector. Add,
  move, or remove an entry only when its directory changes; version-only
  changes do not modify it.
- Never edit generated connector code. Change the manifest or generator and run
  the generation check.
- Before changing authorization, operation, Trigger, example, or UI
  configuration, read [configuration guidance](docs/configuration-guidance.md)
  and audit every field visible in Dex Web Connections for every affected
  runnable example.

### Connector-local example and live verification

- Every new connector or user-visible capability includes a checked-in runnable
  example. A behavioral fix extends the smallest example that proves the fix.
  Documentation-only, generated-only, or internal refactors may rely on an
  existing example only when it still covers the unchanged public path and the
  pull request records why no example change was needed.
- Treat that example as the minimal consuming application and primary end-to-end
  verification path. Run it against a real Dex stack, not only a fake provider
  or headless routing check.
- When safe provider authorization is available, exercise the same example
  against the real provider through the user-controlled Dex Web Connections or
  OAuth flow. Never ask for pasted tokens or browser credentials. Otherwise
  name the exact unverified live behavior and do not claim live or end-to-end
  verification.

### Connector Configuration UX

- Audit the authorization form and every operation and Trigger
  `ConfigurationUI` unit exposed by every runnable example. Every visible
  field has connector-owned guidance; a label or generic type description is
  insufficient.
- Keep provider constants and safe operational defaults in `connector.yaml`.
  Do not make users transcribe endpoints, limits, scopes, or other fixed values.
- Dex Web displays each manifest default below its field as parenthetical
  guidance. Describe its units, valid format, and when an override is useful;
  do not duplicate the literal default in prose.
- Derive identity and resource fields from verified OAuth/OIDC claims or
  declared read-only setup commands whenever the provider can supply them.
  Do not replace derivation with explanatory copy or duplicate free-text input.
- Every credentialed connector declares an authorization guide. For every
  remaining user-supplied field in authorization, operation, or Trigger UI,
  state the provider URL to start from, the exact page path, how to create,
  select, or find the value, its expected format and units, whether it is
  secret, and what blank means.
- OAuth/OIDC setup identifies the provider application page, redirect URI,
  enabled APIs, scopes, and consent requirements before starting authorization.
- `ConnectorUIUnit.Description` is required and explains the choice in the
  context of that operation or Trigger, including provider-derived picker
  output and blank behavior.
- Configuration UI tests enumerate visible fields and cover instructional
  links, derived outputs, parenthesized defaults, validation, and secret-safe
  rendering.

### Optional OAuth/OIDC Authorization Evidence

- This evidence applies only when adding a connector or operation whose manifest
  sets `spec.auth.type: oauth2`, including OAuth 2.0 and OpenID Connect.
- Open the pull request before requesting live authorization testing or evidence.
  Evidence is recommended, never a prerequisite for opening or early review.
- After opening the pull request, remind the author to test the real provider
  application, API, or scope configuration. They may attach a screenshot to the
  pull request description showing the provider and enabled APIs or scopes.
- API keys, manually entered tokens, webhook secrets, and other static
  credentials do not need this recommendation.
- Redact tokens, client secrets, account details, and unrelated personal data.
  Do not commit authorization screenshots to the repository.
- If live authorization testing is incomplete, say so plainly. Never fabricate
  authorization evidence or claim unperformed setup.
- Reviewers treat the evidence as advisory and do not block early connector
  sharing solely because it is absent.

## Dex Semantics and Skill Routing

- Before changing Connector Step options, branches, retry, durability, Trigger
  delivery, Flow examples, or other Dex semantics, load the installed `dex-sdk`
  skill through the host's native skill mechanism and follow its Core and Go
  guidance. This repository owns the connector-specific rules.
- If the skill is unavailable, stop the Dex application-modeling portion and
  follow https://docs.superdurable.io/build-with-ai/dex-developer-skill.
- Only a happy-path branch is required. Mark every other branch `optional: true`.
  An unwired optional branch fails the Flow when selected. Require another
  branch only when the application must choose a continuation, and explain why.
- Execute durability defaults to async. Use sync only when the operation is very
  likely to exceed seven seconds. An LLM generation call may use sync. Do not
  choose sync merely because the Execute timeout ceiling is 30 seconds.
- Every Step type in a Flow Definition Graph 2.0 source declares exactly one
  `// dex:explanation text:"..."` directive beside its `dex:group`.
- Application RPC handler and explicit RPC names begin with concrete verbs such
  as `get`, `list`, `describe`, `send`, `update`, `delete`, or `move`.
- Python examples do not use `del` merely to mark required callback parameters
  or local values as unused.

## Versioning and Release Order

- Connector manifest `metadata.version` is the release source of truth. Leaving
  it unchanged explicitly defers release.
- A declared connector version equals the latest tag or advances by one patch,
  minor, or major version. First releases use `v0.1.0`.
- Core SDK tags use `sdkgo/vX.Y.Z`. Connector tags use the module directory,
  such as `connectors/openai/vX.Y.Z`.
- Connector modules require an exact published Connector Go SDK release.
- Dex Server is backward compatible with every earlier Dex SDK release. Pin no
  Dex Server, Dex Go SDK, or Dex CLI version; record minimums instead. A
  module's `go.mod` records a minimum `github.com/superdurable/dex/sdk-go`
  release. Raise it only when the module needs a newer release, then to the
  newest stable release, after reading that release's Breaking Changes section.
- Use only released Dex versions: no prerelease, pseudo-version, or `replace`.
- CI resolves the latest stable Dex CLI and Dex Go SDK on every run. Set
  `DEX_CLI_VERSION` only to reproduce a failure.
- A connector may require another connector module only at an exact released
  tag that is reachable from `main` and whose GitHub release has
  `connector-release.complete`. Release the dependency first and pin it in a
  later PR.
- From this repository, connector modules may require only the SDK and other
  connector modules, never the root module or an `examples/` module.
- Connector `go.mod` files must not contain `replace`, pseudo-versions, branches,
  or commit SHAs.
- If a connector needs an SDK change, release the SDK first. Upgrade connectors
  in later PRs only after that SDK tag exists.
- During discovery, use the ignored `go.work` from `make workspace` or a
  temporary local `replace` and run full connector verification. Never commit
  that `replace`.
- Test every module with `GOWORK=off` before release.
- Connector releases run automatically after merge to `main`; manual runs only
  repair incomplete releases or catalog deployment.
- A v0 breaking release cannot be a patch. A v1+ breaking release requires a
  major-version module-path migration.

## PR, Commit, and Branch Rules

- Use one released module per PR unless a repository-wide mechanical change
  requires several.
- Use `sdkgo:`, `connector(<slug>):`, `tooling:`, or `docs:` subjects.
- Put the exact lowercase token `(breaking)` in the PR title or body and final
  commit message for every public breaking change.
- Fetch and branch from the latest `origin/main`. See
  `.cursor/rules/git-branch.mdc`.
- End every changing agent turn with one commit and a clean working tree. Do not
  create empty commits for discussion-only turns.
- Preserve unrelated user files and changes.
- Never add an AI tool as Author, Committer, or `Co-authored-by`. Never use
  `--no-verify`. Verify the final message with `git log -1 --format=%B`.

## Agent Rule Synchronization

Keep Cursor (`.cursor/rules/`), Codex (`AGENTS.md` and `.codex/rules/`), and
Claude (`CLAUDE.md`) rules equivalent. Update all affected mirrors in one
commit.

## Code Quality

### Naming and API Shape

- Prefer complete, precise domain names for APIs, interfaces, classes, types,
  methods, functions, fields, variables, and constants. Brevity is not a goal.
- Avoid generic names such as `Get`, `Update`, `Manager`, `Data`, or `Handler`
  when a domain-specific name is available at the call site.
- Boolean variables, internal fields, and boolean-returning helpers use clear
  predicates such as `isXxx`, `hasXxx`, `canXxx`, `shouldXxx`, or
  `supportsXxx`. Stable public and wire names remain unchanged.
- Do not introduce `NormalizeXyz`. Name the operation precisely, such as
  `TrimWhitespace`, `CanonicalizeURL`, or `ValidateAndSortSelections`.
- Do not use vague names such as `normalize`, `normalizer`, or `runtime` for
  packages, directories, files, classes, structs, interfaces, fields,
  parameters, variables, or helpers. Name the exact domain behavior, owned
  state, or responsibility.
- Reuse existing repository and public API terms instead of inventing synonyms.
- Variables, including fields and parameters, use descriptive names. Go method
  receivers and `i j k n err ctx ok t mu wg id r w ch` are allowed exceptions.
- Design public APIs so call sites read naturally in their host language.

### Construction and Structure

- Use constructor injection. Never add setter, `Inject*`, `Wire*`, or exported
  mutable-field injection after construction.
- Pass a pointer to the component's config section instead of individual
  tunables or an unrelated root config object. Fail fast on a required nil
  dependency.
- Lift a closure into an explicit struct method when it captures three or more
  values, mutates captured state, has multiple call sites, or outlives one
  statement. One-shot callbacks and tiny pure transforms are fine.
- Keep a struct's methods in one primary file.
- In Go files, callers precede callees: types and constructors first, then the
  main entry path, handlers, mutators, converters, and leaf helpers.
- Use a package's declared name. Alias imports only for a collision, ambiguity,
  or an established repository convention.
- Required dependencies fail fast. Check nil only when nil is an expected state.

### Comments and Public API Documentation

- New comments explain only a non-obvious reason, trade-off, invariant, or
  external constraint. Prefer a precise name over an obvious comment.
- Keep each new contiguous comment block under 20 words unless the user asks for
  detail. Return-value documentation and public SDK/API documentation are
  exempt.
- Preserve existing comments verbatim during refactors. Move them with their
  code. If behavior makes one stale, change only the outdated references.
- Document every hand-written public SDK and connector type, interface, method,
  function, constructor, field, constant, enum value, and equivalent construct.
  Do not add documentation to generated or non-public declarations.
- Public API docs start with a summary and explain relevant defaults, units,
  lifecycle, concurrency, ownership, side effects, errors, and limitations.
  Give a complete Go example for each related API family in package docs,
  READMEs, or runnable examples.

### Errors, Ownership, and Files

- Handle every returned error by returning, logging, or explicitly acting on it.
  A deliberately ignored best-effort error needs a short reason.
- Treat provider/client input and persisted external files as untrusted: validate
  and return an error. Trusted internal invariants may use a named `Must*`
  helper or fail fast.
- Do not deep-copy messages defensively. Copy only when an algorithm needs an
  independently mutable value or a public API promises isolation.
- Before producing a binary, add its exact output path to `.gitignore` and
  remove stray uncommitted binaries.
- Every new or edited hand-written Go, Python, TypeScript, TSX, CSS, and HTML
  source starts with the repository MIT SPDX header. Skip generated files.

## Go Connector SDK Conventions (`sdkgo/`)

- Factory and constructor names include the domain noun when package context is
  insufficient.
- Identity types use `*ID`, not `*Ref`, unless the value is specifically a
  logical reference such as `ConnectionRef` or `OperationRef`.
- Optional fields use pointers when zero is a meaningful distinct value.
- Names describe the actual semantic action.
- Keep public APIs thin and omit parameters the SDK can derive safely.
- Do not wrap a few flat inputs in an options struct. Use options for a coherent
  extensible configuration surface.
- Functional options use sealed apply methods, never marker interfaces plus a
  type switch.
- Schema-erasure interfaces are sealed with unexported methods; application
  names remain on typed values.
- When an entry method needs a recursive or stateful helper, use a precise
  `doXxx` or domain-specific name and keep the entry method thin.

## Tests

- Run Go suites and vet through the repository Makefile. Test every standalone
  module with `GOWORK=off`.
- Use a real Dex integration test for behavior crossing a Worker, Client,
  persistence, wait, retry, Trigger, RPC, Attribute, or Stream boundary.
- Use unique Flow IDs and convergence polling. Never use a fixed sleep to guess
  when asynchronous state has converged.
- A fixed wait is allowed inside the system under test or when elapsed time is
  itself the behavior being verified.
- Never skip, gate, or early-return around a failing assertion merely to green
  the suite. Diagnose the root cause and fix product behavior or expectations.
- Keep provider fakes credential-safe and assert retry, duplicate, terminal,
  recovery, and unknown-outcome behavior where relevant.

# Dex Connectors Library — Codex Instructions

Open-source Dex-native connector SDKs, provider connectors, manifests, code
generation, React primitives, and examples.

## Plan Mode

Every implementation plan includes Tests, Documentation, and UI/UX. Prefer
real Dex integration tests for Step, retry, Stream, Attribute, and transition
behavior.

## Repository Boundaries

- Keep the Go SDK in sdkgo and every connector in its own Go module.
- Group one company's connectors below one directory, such as
  connectors/google/gmail and connectors/google/spreadsheet.
- Keep provider calls in Dex Step Execute. Do not call providers from RPCs.
- Preserve stable Flow, Step, Attribute, Stream, connector, operation, and
  branch identities.
- Register every Attribute and Stream in the consuming Flow persistence schema.
- Keep secrets out of Flow input, Attributes, Results, receipts, Streams, logs,
  generated values, and release artifacts.

## Connector Authoring

- connector.yaml is the source for company, release version, generated Config,
  Credentials, definitions, branch fields, operation-specific Step factories,
  and defaults.
- Normal application APIs use operation-specific factories such as
  openai.NewCreateResponseStep.
- Generic sdkgo.NewQueryStep and NewMutationStep are advanced escape
  hatches.
- Every connector has its own go.mod, README, manifest, generated code, and
  provider tests.
- Register every connector directory in the sorted root connectors.yaml list.
  Paths may have any depth below connectors/. The first directory is the
  company, must match metadata.company, and must contain logo.svg.
- Add, move, or remove a registry entry only when the connector directory
  changes. Version-only changes do not modify the registry.

## Versioning and Release Order

- Connector manifest metadata.version is the release source of truth. Leaving
  it unchanged explicitly defers release.
- A declared connector version must equal the latest tag or the next patch,
  minor, or major version. First releases use v0.1.0.
- Core SDK tags use sdkgo/vX.Y.Z. Connector tags use the module directory,
  such as connectors/openai/vX.Y.Z.
- Connector modules require an exact published Connector Go SDK release.
- Connector go.mod files must not contain replace, pseudo-versions, branches,
  or commit SHAs.
- If a connector needs an SDK change, deliver and release the SDK first. Upgrade
  connectors in later PRs only after that SDK tag exists.
- While discovering an SDK contract gap, develop the SDK and connector together
  with `go.work` or a temporary local `replace` and run the full connector
  verification. Never commit that `replace`. Before handoff, split the SDK
  changes into a preceding SDK PR, merge and release its tag, then make the
  connector PR pin that exact published SDK version.
- Before release, test each module with GOWORK=off.
- Connector releases run automatically after merge to main. Manual runs only
  retry incomplete releases and catalog deployment.
- A v0 breaking release cannot be a patch. A v1+ breaking release requires a
  major-version module-path migration before release.

## PR and Commit Messages

Use one released module per PR unless a repository-wide mechanical change
requires several. Use these title and commit-subject scopes:

- sdkgo: ...
- connector(openai): ...
- connector(google/gmail): ...
- tooling: ...
- docs: ...

Put the exact lowercase token (breaking) in the PR title or body and the final
commit message for every public breaking change. Release tooling places
matching changes in the Breaking Changes section.

## Agent Rule Synchronization

Keep AGENTS.md, CLAUDE.md, and .cursor/rules/*.mdc equivalent. Change all three
agents' rules in the same commit.

## Commits and Branches

Fetch and branch from the latest origin/main. End every changing turn with one
commit and a clean working tree. Preserve unrelated user files and changes.
Never add an AI tool as Author, Committer, or Co-authored-by, and never use
--no-verify.

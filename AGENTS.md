# Dex Connectors Library — Codex Instructions

Open-source Dex-native connector SDKs, provider connectors, manifests, code
generation, React primitives, and examples.

## Plan Mode

Every implementation plan includes Tests, Documentation, and UI/UX. Prefer
real Dex integration tests for Step, retry, Stream, Attribute, and transition
behavior.

## Repository Boundaries

- Keep the Go SDK in sdk/go and every connector in its own Go module.
- Group one company's connectors below one directory, such as
  connectors/google/gmail and connectors/google/spreadsheet.
- Keep provider calls in Dex Step Execute. Do not call providers from RPCs.
- Preserve stable Flow, Step, Attribute, Stream, connector, operation, and
  branch identities.
- Register every Attribute and Stream in the consuming Flow persistence schema.
- Keep secrets out of Flow input, Attributes, Results, receipts, Streams, logs,
  generated values, and release artifacts.

## Connector Authoring

- connector.yaml is the source for generated Config, Credentials, definitions,
  branch fields, operation-specific Step factories, and defaults.
- Normal application APIs use operation-specific factories such as
  openai.NewCreateResponseStep.
- Generic connector.NewQueryStep and NewMutationStep are advanced escape
  hatches.
- Every connector has its own go.mod, README, manifest, generated code, and
  provider tests.
- A new, moved, or removed connector must regenerate the release workflow
  choice list. Never edit that generated list by hand.
- Run the release-workflow generation check before committing connector catalog
  changes.

## Versioning and Release Order

- Git tags are the only version authority. Source manifests do not contain a
  release version.
- Core SDK tags use sdk/go/vX.Y.Z. Connector tags use the module directory,
  such as connectors/openai/vX.Y.Z.
- Connector modules require an exact published Connector Go SDK release.
- Connector go.mod files must not contain replace, pseudo-versions, branches,
  or commit SHAs.
- If a connector needs an SDK change, deliver and release the SDK first. Upgrade
  connectors in later PRs only after that SDK tag exists.
- Before release, test each module with GOWORK=off.
- Release only from main with the generated GitHub workflows.
- A v0 breaking release cannot be a patch. A v1+ breaking release requires a
  major-version module-path migration before release.

## PR and Commit Messages

Use one released module per PR unless a repository-wide mechanical change
requires several. Use these title and commit-subject scopes:

- sdk(go): ...
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

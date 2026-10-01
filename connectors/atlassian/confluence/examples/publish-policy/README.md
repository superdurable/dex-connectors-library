# Confluence policy publication example

This example runs one operation-only Flow from Dex Web **Start Flow** and uses
every Confluence operation to publish an in-force policy:

1. `RecordPublicationRequest` validates the request and records it with the
   picked space;
2. `FindRelatedPolicies` calls `confluence.NewSearchPagesStep` for pages in the
   space whose title contains the requested title, and `RecordRelatedPolicies`
   keeps up to ten for the record. Search is eventually consistent, so it never
   decides whether to create or update;
3. `PublishPolicyPage` calls `confluence.NewCreatePageStep`. `created` reads the
   new page back. `titleConflict` means a page with that title already exists,
   so `RecordExistingPage` updates it instead. `notFound` and
   `providerRejected` complete as `rejected`;
4. `UpdatePolicyPage` calls `confluence.NewUpdatePageStep` with the existing
   page's version plus one. `versionConflict` means another editor saved a
   newer version first; the Flow parks for an operator instead of overwriting
   it;
5. `ReadBackPolicyPage` calls `confluence.NewGetPageStep`, and
   `VerifyPublishedPolicy` checks the title and version and records the start
   of the page as Markdown;
6. `AddPublicationComment` calls `confluence.NewAddCommentStep`. Both `added`
   and `uncertain` complete; an uncertain comment is recorded and never re-sent.

A blank `publicationComment` skips the comment. Unwired optional branches, such
as a rejected search or a page that cannot be read back, fail the Flow.

## Reviewing an edit conflict

When the update finds a version it did not write, the Flow records that version
in `conflictingVersionNumber`, sets the phase to `needsConflictReview`, and
waits. An operator reviews the edit in Confluence and runs **Publish over
latest version**, which requires `confluence-policy-publication.publish`. It is
the only path that writes over someone else's edit, as a new Step execution
with a new connector call ID.

## Configure

Follow the [Confluence connector setup](../../README.md), then configure the
`confluence-policies` connection under **Connections** in Dex Web. Leave
`cloudId` blank when the authorization covers one Confluence site. The
**Policy space** picker on the `PublishPolicyPage` Step saves the space; the
search uses the same space. Leave it unsaved to use each Start Flow input's
`spaceKey`. The Worker reads the saved value once at startup, so restart it
after saving.

## Run

Generate strict FDG 2.0 from `connectors/atlassian/confluence` with the dexcli
release pinned in the repository's `.dex-compat-version` file:

```bash
mkdir -p build
dexcli visualize ./examples/publish-policy/flow/workflow.go \
  --schema-version 2.0 --json --out ./build/publish-policy
```

Run `dexcli dev` with that build directory, then run the Worker with the
connection path shown by Dex Web:

```bash
export DEX_CONNECTOR_CONFIG_FILE="$HOME/.dex/connectors/connections.json"
go run ./examples/publish-policy
```

The default Worker address is `127.0.0.1:8836`. Override
`DEX_FLOW_SERVICE_ADDRESS`, `DEX_WORKER_BIND_ADDRESS`, or `DEX_BLOB_CACHE_DIR`
when needed.

Start `ConfluencePolicyPublication` with a unique Flow ID:

```json
{
  "title": "Remote work policy",
  "body": "# Remote work\n\nStaff may work **remotely** two days a week.\n\n- Ask your manager\n- Log the days",
  "spaceKey": "OPS",
  "changeSummary": "Annual review",
  "publicationComment": "Published for the **Q4** review."
}
```

The Flow result and the `confluence-policy-publication` Attribute hold the
phase, the related policies, the page ID, version, and web URL, the read-back
text, and the comment ID.

## Test

```bash
GOWORK=off go test -race ./examples/publish-policy/...
```

With the pinned Dex development server running, the integration test drives
the Flow on a real Worker against a stateful fake Confluence that keeps one
page per title and accepts each version number once: a new policy published,
read back, and commented once; an existing title updated to its next version;
a nine-second create sent once under sync durability; a nine-second update
whose async backup attempt is refused and finds its own version; a create
timeout confirmed by title without a second page; a rate-limited create retried
after `Retry-After`; a rejected create; a concurrent edit parked and published
over only after the Action; a nine-second comment sent once; a comment timeout
confirmed by reading the comments back without a resend; and a blank `cloudId`
resolved to the only granted site.

```bash
GOWORK=off go test -tags=integration ./examples/publish-policy/... -count=1 -v
```

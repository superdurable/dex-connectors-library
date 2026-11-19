# Mailchimp TypeScript connector

`@superdurable/connector-intuit-mailchimp` provides typed Dex operations for reading and listing audience contacts, upserting contacts, updating tags, and sending a reviewed campaign, and the `memberChanged` poll Trigger. It requires `@superdurable/connectors-sdk` 0.12.0, the release of the poll Trigger kit.

Install with `npm install @superdurable/connector-intuit-mailchimp@0.2.1`. Configure a Mailchimp API key in Dex Web as described in the [connector README](../README.md#mailchimp-setup). The package derives the data center from the key suffix and never puts the key in Flow input or output.

The runnable [get-member example](examples/get-member/README.md) shows the static connection name, Worker, and Flow. Its integration test uses a local Mailchimp stand-in and a real `dexcli dev` server. The Go-recorded [conformance scenarios](../../../../conformance/connectors/intuit/mailchimp/scenarios.json) cover all five operations, including retries and campaign send recovery.

For `sendCampaign`, the send runs under the SDK single-dispatch fence with the manifest's `maxSends` (3 unless the application overrides it). A 429, a throttling 403, a 503, or a connection failure proven to occur before the send is retried without counting the send. Any other outcome after the send is unknown: it is retried while another send is allowed, and the retry reads campaign status first, reporting `alreadySent` when Mailchimp shows `sending` or `sent` and resending only a draft. At the cap it reports `uncertain`. See [Duplicate safety](../README.md#duplicate-safety).

## memberChanged Trigger

`memberChanged` delivers each contact added to or changed in one audience, at behavior parity with the Go connector; the [connector README](../README.md#memberchanged-trigger) describes the baseline, lookback, paging, event IDs, status filter, rate limits, and cursor resets. The poll cursor token, the keyed snapshot entries, and the Trigger event IDs match Go byte for byte, so Go and TypeScript replicas share one cursor document and one Trigger inbox; `test/member_changed_go_parity.test.ts` replays a recording of the Go fetcher, including HTML-escaped and non-ASCII merge fields, numbers in exponent form, reordered tags, paging by time and by offset, and every failure class.

- `newProjectMemberTriggerRunner(project, connection, routes)` loads each binding's saved `audienceId` and `statuses` and records each change in the binding's project Trigger inbox before its cursor commits, as [`examples/member-changed/app.ts`](examples/member-changed/app.ts) does. A binding without a saved audience throws.
- `newMemberTriggerRunner(connection, routes)` takes routes with a configuration, a target, and an optional cursor store.
- `newMemberChangedTrigger(...)` keeps its cursor in memory, like Go's generated `NewMemberChangedTrigger`, so it records a new baseline on every start.

`newProjectConnection` reads the connection's `pollInterval` (at least 5 seconds); `MailchimpClient` takes it as `pollIntervalMs` and throws below `MinimumPollIntervalMs`. The runner sends one request at a time for every binding of a connection. A 429, or a 403 without an error document, is `markRateLimited` with `Retry-After`; a 400 for the poll query, an undecodable cursor, or a cursor for another audience is `markCursorInvalid`, which records a new baseline. `MemberChangedEventSchema` and `MemberChangedTriggerConfigurationSchema` are zod schemas with the Go JSON fields.

The [member-changed example](examples/member-changed/README.md) starts one `MailchimpMemberChangeTypeScript` Flow per change and reads the contact back with `newGetMemberStep`.

Live Mailchimp provider authorization and behavior have not been verified for this TypeScript package, nor have the Trigger's live assumptions: whether `since_last_changed` is exclusive at one-second resolution, how soon a change becomes listable, and whether Mailchimp ever sends `Retry-After`.

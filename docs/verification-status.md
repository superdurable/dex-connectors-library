# Verification Status

This page records how far each catalog connector has been verified against its
real provider. The levels follow the wording in
[acceptance](acceptance.md#connector-local-example-and-live-verification): a connector that has not run
against its provider is "Dex-integrated verified, not live" or "structurally
verified", never live or end-to-end verified.

| Level | Meaning |
|---|---|
| **Live** | The checked-in example or operations ran against the real provider with real credentials. |
| **Partial live** | Only limited contact with the real provider, such as invalid-credential probes that confirmed error shapes, or a compatible server standing in for the product. |
| **Dex-integrated** | Ran on a real Dex stack, through Dex Web or `dexcli dev`, against a local stand-in for the provider. |
| **Structural** | Unit, manifest, contract, and graph checks only. No Dex run is recorded for the current code. |

Evidence comes from each connector's README and the pull requests that added or
changed it. When a pull request performs a live test, update the connector's
row in the same pull request. Each connector README lists its unverified live
behavior in full; the last column below gives only the main gaps.

No connector has had its OAuth token refresh exercised against a real token
endpoint. This includes the `sdkgo/oauthtoken` migrations (#127 to #132).

Last reviewed against `main` at `6a0dfc1` (2026-10-01).

## Summary

| Level | Count | Connectors |
|---|---|---|
| Live | 7 | github, google/gemini, hackernews, mysql, postgresql, slack, spotify |
| Partial live | 8 | amazon/s3, atlassian/trello, calendly, google/gmail, intercom, monday, typeform, xero |
| Dex-integrated | 31 | every other connector except the three below |
| Structural | 3 | google/spreadsheet, linkedin, stripe |

## Connectors

| Connector | Level | What was exercised | Main gaps | PRs |
|---|---|---|---|---|
| airtable | Dex-integrated | Dex Web Start Flow run and 6 real-Dex tests against a fake | upsert match errors, 10-record write cap, 429 timing, pickers on a real base | #155 |
| alibaba/qwen | Dex-integrated | real-Dex tests against a fake; the opt-in live test has no recorded run | any real Model Studio call, model picker, regional endpoints | #83 |
| amazon/s3 | Partial live | live suite and the example on real Dex against MinIO RELEASE.2025-10-15 | real Amazon S3 and Cloudflare R2, session tokens, conditional writes, Object Lock | #167 |
| anthropic | Dex-integrated | real-Dex tests against a fake | any real Messages API call, model picker | #75 |
| asana | Dex-integrated | Dex Web Start Flow run and 11 real-Dex scenarios against a fake | real response shapes, date filters, the three pickers | #147 |
| atlassian/confluence | Dex-integrated | 11 real-Dex runs against a fake; Dex Web built the authorize URL | OAuth code exchange and refresh, title lookup, stale-version status, pickers | #159 |
| atlassian/jira | Dex-integrated | Dex Web Start Flow run and 11 real-Dex tests against a fake | OAuth code exchange and refresh, enhanced-search paging, pickers | #148 |
| atlassian/trello | Partial live | real Trello rejected dummy credentials in a Dex Web run; the rest against a fake | every valid-credential response, list-replace semantics, length limits | #156 |
| bamboohr | Dex-integrated | 3 Dex Web Start Flow runs and 18 real-Dex tests against a fake | change-history bounds, rate-limited adds, future-start hires | #165 |
| calendly | Partial live | picker commands reached Calendly and failed cleanly with a placeholder token; Trigger run with locally signed deliveries | OAuth code exchange, webhook retries, pickers on a real account | #149 |
| deepseek | Dex-integrated | real-Dex tests against a fake | any real API call, model picker, 402 balance handling | #79 |
| freshworks/freshdesk | Dex-integrated | Dex Web Start Flow run and 14 real-Dex scenarios against a fake | contact lookup, tag replacement, pagination, 429 | #145 |
| github | Live (reads) | OAuth App authorized through Dex Web; the three read operations ran against github.com | OAuth refresh (#117, #129), rate-limit retries, GitHub Enterprise | #64 |
| google/calendar | Dex-integrated | 6 real-Dex scenarios against a fake Google | OAuth consent and refresh, delegation, Meet creation, calendar picker | #151 |
| google/docs | Dex-integrated | 10 real-Dex scenarios against a fake Google | OAuth scopes, Markdown import, stale-revision error, pickers | #166 |
| google/drive | Dex-integrated | Dex run ended `copied` against a fake Drive | OAuth refresh, delegation, shared drives, uploads into folders the app did not create | #150 |
| google/forms | Dex-integrated | two Dex Web Start Flow runs against a fake Google | `timestamp` filter, listing order, form picker, OAuth consent | #169 |
| google/gemini | Live | a Slack-triggered Process ran five stages against the real Gemini API; real 400, 404, and 402 observed | `generateText` (#96) not run live; thinking-budget and blocked branches tested against a fake only | #63 |
| google/gmail | Partial live | a real Google OAuth client reproduced a scope error | any real send or read, Trigger polling, token refresh | #89 |
| google/spreadsheet | Structural | unit tests against `httptest` fakes | real Google authorization (#116), every Sheets call, picker, refresh (#127) | #7 |
| google/workspace-admin | Dex-integrated | 8 real-Dex scenarios against a fake Google; the Dex Web run stopped at the first Google call (no real tenant) | everything against a real tenant: delegation, group-membership statuses | #170 |
| hackernews | Live | Dex Web connection Ready; a live digest ran through Start Flow | one manual run only; no automated live test | #88 |
| helpscout | Dex-integrated | Dex Web run against a fake | client-credentials token errors, reply rendering, picker | #161 |
| hubspot | Dex-integrated | two Dex runs against a fake | real response and error shapes, OAuth (expected to fail on `scope` versus `scopes`), pickers | #143 |
| intercom | Partial live | placeholder-token requests to all three regional hosts returned 401 | every response to a valid token, real webhook signatures | #152 |
| intuit/mailchimp | Dex-integrated | Dex Web run made exactly one send against a fake | Bearer on every endpoint, campaign send state, tag automations | #164 |
| linkedin | Structural | unit tests; the earlier real-Dex example was replaced and no integration test remains | OIDC consent, UserInfo, refresh | #10, #119, #130 |
| meta | Dex-integrated | real-Dex tests against a fake | any real api.meta.ai call, model picker | #73 |
| mistral | Dex-integrated | real-Dex tests against a fake | any real call, model picker, EU and US endpoints | #78 |
| monday | Partial live | an unknown-client probe of the real OAuth token endpoint returned `invalid_client`; Dex Web runs against a stand-in | OAuth code exchange, error shapes, daily limit | #157 |
| moonshot/kimi | Dex-integrated | real-Dex tests against a fake | any real call, both endpoints, model pickers | #80 |
| mysql | Live | live suite on local MySQL 8.4.11 and MariaDB 13.0.2; Dex example run `recorded` then `alreadyRecorded` | managed services (RDS, Cloud SQL, Azure), other server versions, `verify-identity` | #163 |
| notion | Dex-integrated | Dex Web Start Flow runs against a fake Notion | every live Notion response | #139 |
| openai | Dex-integrated | real-Dex tests against a fake; `-tags=live` not run | live Responses API calls, model picker | #97 |
| postgresql | Live | local PostgreSQL 17.11 with TLS; Dex runs `recorded` and `alreadyRecorded`, one row on read-back | RDS, Supabase, Neon, a `verify-full` CA chain, PgBouncer | #140 |
| salesforce | Dex-integrated | four Dex Web Start Flow runs against a fake | error codes mapped without the docs (they returned 403), OAuth, JWT bearer | #142 |
| slack | Live | OAuth, Trigger, RPC approval, and reply in a real Slack workspace | token rotation and refresh (#118, #132), pickers against live Slack | #40, #58 |
| spotify | Live | OAuth PKCE completed; a real playlist page returned through a Dex Flow | refresh (#120, #131), playlists the user does not own | #85 |
| stripe | Structural | unit tests, signed webhook fixtures, compatibility gate; no Dex run recorded | test-mode Checkout, live webhook delivery | #66, #136 |
| superdurable/email | Dex-integrated | Dex Web Start Flow run against in-process IMAP and SMTP servers | Fastmail, iCloud, and Yahoo logins; search semantics; Sent-folder saving | #171 |
| superdurable/llm | Dex-integrated | real-Dex tests against fake providers; `-tags=live` not run | live generation with real keys, the multi-provider form in Dex Web | #123 |
| superdurable/webhook | Dex-integrated | Dex Web connection and run; a forged request was rejected; senders simulated locally | real GitHub, Shopify, and Typeform senders; public HTTPS ingress | #137 |
| twilio/messaging | Dex-integrated | Dex runs and 7 real-Dex tests against a fake Twilio | real response and status codes, regional endpoints | #141 |
| typeform | Partial live | a dummy-token request to the real API returned 403 | webhook `event_id` stability, answer shapes, 429 | #158 |
| xai | Dex-integrated | real-Dex tests against a fake | any real call, model picker | #77 |
| xero | Partial live | invalid-credential probes confirmed Xero's 400 and 401 shapes; Dex Web run against a fake | OAuth consent, rate-limit headers, idempotency caching | #168 |
| zendesk/support | Dex-integrated | Dex Web connection and two completed runs against a fake | search shape and cursors, concurrent creates | #144 |
| zoho/desk | Dex-integrated | Dex Web Start Flow run and 15 real-Dex tests against a fake | scope acceptance, search quirks, organization picker | #160 |
| zoom | Dex-integrated | 11 real-Dex tests against a fake; Dex Web built the authorize URL | OAuth code exchange and refresh, real response shapes | #146 |

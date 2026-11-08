# Connector expansion candidates: 100 providers

Snapshot: 2026-11-07. This is a qualitative priority backlog, not a usage or revenue ranking. Zapier exposes a public “most popular” app ordering and popular/trending lists; n8n exposes a public integration directory and category pages. Neither publishes comparable per-app usage counts. The order below combines their discovery signals with common automation use cases and Dex's existing coverage. The proposed capabilities are product candidates, not verified provider API contracts or committed Dex operation names.

Sources: [Zapier app directory](https://zapier.com/apps), [Zapier popular forms](https://zapier.com/apps/categories/forms), [n8n integration directory](https://n8n.io/integrations/), [n8n AI category](https://n8n.io/integrations/categories/ai/), [Zapier Facebook Lead Ads capabilities](https://zapier.com/apps/facebook-lead-ads/integrations), [n8n MongoDB capabilities](https://n8n.io/integrations/mongodb/), and [n8n Anthropic capabilities](https://n8n.io/integrations/anthropic/). Provider-specific API and authorization requirements must be checked before implementation.

## Existing library and language gap

The checked-in catalog has 102 connectors. All 102 manifests declare Python and TypeScript packages. Across their manifests, all 363 Go operations appear in both language lists. Go declares 13 Triggers across 11 connectors; Python and TypeScript declare none. Thus the immediate Python/TypeScript parity backlog is Trigger delivery in DocuSign, Linear, Help Scout, Intercom, Typeform, ClickUp, Slack (two), Calendly, Stripe, Superdurable Webhook, and Gmail (two). These counts describe manifest declarations, not live provider verification or release completion.

The 100 entries below are **new connector candidates** absent from `catalog.yaml`; they do not count the 11 existing Trigger ports. Each row suggests an initial provider query, mutation, or Trigger. `Q` means a read-only query, `M` a provider mutation, and `T` a provider event source. A candidate Trigger should be included only after its event transport, duplicate handling, and permissions are verified. Go remains the reference implementation; Python and TypeScript follow the Go-verified provider scenarios and may initially expose a documented subset.

## Tier A — strongest general-purpose demand

| # | Connector candidate | Suggested first capabilities |
|---:|---|---|
| 1 | Telegram Bot | T: receive message; M: send message; Q: get chat |
| 2 | WhatsApp Business Platform | T: receive message; M: send template message; M: send text message |
| 3 | Facebook Lead Ads | T: new lead; Q: get lead; Q: list lead forms |
| 4 | Facebook Pages | T: new page post; M: publish page post; Q: list posts |
| 5 | Instagram for Business | T: new comment; M: publish media; Q: list media |
| 6 | Dropbox | T: file created; Q: download file; M: upload file |
| 7 | Box | T: file event; Q: download file; M: upload file |
| 8 | WordPress | T: post published; Q: get post; M: create post |
| 9 | Microsoft Dynamics 365 Sales | T: record changed; Q: find contact; M: create or update lead |
| 10 | Adobe Marketo Engage | T: lead created; Q: get lead; M: create or update lead |
| 11 | ActiveCampaign | T: contact added; Q: get contact; M: create or update contact |
| 12 | Constant Contact | T: contact added; Q: get contact; M: create or update contact |
| 13 | SurveyMonkey | T: response completed; Q: get response; Q: list surveys |
| 14 | Jotform | T: submission created; Q: get submission; Q: list forms |
| 15 | Eventbrite | T: attendee registered; Q: get attendee; Q: list events |
| 16 | Anthropic | M: create message; Q: get model; M: upload file |
| 17 | Google BigQuery | Q: run query; M: insert rows; Q: get job |
| 18 | Firebase Firestore | T: document changed; Q: get document; M: upsert document |
| 19 | Redis | Q: get key; M: set key; M: publish message |
| 20 | MongoDB | T: collection change; Q: find documents; M: upsert document |
| 21 | Elasticsearch | Q: search documents; M: index document; M: delete document |
| 22 | Amazon DynamoDB | T: stream record; Q: get item; M: put item |
| 23 | Amazon SNS | M: publish message; Q: list topics; M: create subscription |
| 24 | Amazon SES | M: send email; Q: get send quota; Q: get identity status |
| 25 | Amazon Lambda | M: invoke function; Q: get function; Q: list functions |

## Tier B — broad automation and developer workflows

| # | Connector candidate | Suggested first capabilities |
|---:|---|---|
| 26 | Azure Blob Storage | T: blob created; Q: download blob; M: upload blob |
| 27 | Azure DevOps | T: work item changed; Q: get work item; M: create work item |
| 28 | Google Cloud Storage | T: object created; Q: download object; M: upload object |
| 29 | Google Cloud Pub/Sub | T: subscription message; M: publish message; Q: get topic |
| 30 | TikTok for Business | Q: list campaigns; Q: get ad report; M: create campaign |
| 31 | X (Twitter) | T: mention received; Q: get post; M: create post |
| 32 | Reddit | T: new post in subreddit; Q: get post; M: create comment |
| 33 | Pinterest | T: new pin; Q: list pins; M: create pin |
| 34 | Buffer | Q: list channels; M: create queued post; Q: list queued posts |
| 35 | Hootsuite | Q: list social profiles; M: schedule post; Q: list scheduled posts |
| 36 | LinkedIn Ads | Q: list campaigns; Q: get campaign analytics; M: create lead form |
| 37 | Microsoft To Do | T: task created; Q: list tasks; M: create task |
| 38 | Wrike | T: task changed; Q: get task; M: create task |
| 39 | Basecamp | T: to-do created; Q: list to-dos; M: create to-do |
| 40 | Teamwork.com | T: task changed; Q: get task; M: create task |
| 41 | MeisterTask | T: task created; Q: get task; M: create task |
| 42 | Shortcut | T: story changed; Q: get story; M: create story |
| 43 | Productboard | T: feature changed; Q: list features; M: create note |
| 44 | Miro | T: board item changed; Q: get board; M: create board item |
| 45 | Lucid | Q: list documents; Q: export document; M: create document |
| 46 | Dropbox Sign | T: signature request completed; Q: get request; M: send signature request |
| 47 | PandaDoc | T: document completed; Q: get document; M: create document |
| 48 | Adobe Acrobat Sign | T: agreement completed; Q: get agreement; M: create agreement |
| 49 | OneSpan Sign | T: package completed; Q: get package; M: create package |
| 50 | Google Contacts | T: contact changed; Q: get contact; M: create contact |

## Tier C — marketing, analytics, and customer systems

| # | Connector candidate | Suggested first capabilities |
|---:|---|---|
| 51 | Google Analytics 4 | Q: run report; Q: get property; Q: list audiences |
| 52 | Google Ads | Q: run campaign report; Q: get campaign; M: update campaign status |
| 53 | Meta Ads Insights | Q: get campaign insights; Q: list ad accounts; Q: get ad |
| 54 | Microsoft Advertising | Q: get campaign report; Q: list campaigns; M: update campaign status |
| 55 | Google Search Console | Q: query search analytics; Q: list sites; Q: inspect URL |
| 56 | Google Tag Manager | Q: list containers; Q: list tags; M: create tag |
| 57 | Twilio Segment | T: event received; Q: list sources; M: track event |
| 58 | Amplitude | Q: run event query; Q: get user; M: send event |
| 59 | PostHog | T: event captured; Q: run insight; M: capture event |
| 60 | Heap | Q: run event query; Q: get user; M: identify user |
| 61 | Customer.io | T: campaign event; Q: get customer; M: upsert customer |
| 62 | Braze | T: user event; Q: export user; M: track event |
| 63 | Iterable | T: user subscribed; Q: get user; M: upsert user |
| 64 | Drip | T: subscriber created; Q: get subscriber; M: upsert subscriber |
| 65 | Kit (ConvertKit) | T: subscriber created; Q: get subscriber; M: add subscriber |
| 66 | GetResponse | T: contact created; Q: get contact; M: create contact |
| 67 | Campaign Monitor | T: subscriber added; Q: get subscriber; M: add subscriber |
| 68 | AWeber | T: subscriber added; Q: get subscriber; M: create subscriber |
| 69 | Keap | T: contact created; Q: get contact; M: create contact |
| 70 | Copper CRM | T: lead created; Q: get lead; M: create lead |
| 71 | Insightly | T: opportunity changed; Q: get contact; M: create opportunity |
| 72 | Nimble | T: contact changed; Q: get contact; M: create contact |
| 73 | Freshsales | T: lead created; Q: get lead; M: create lead |
| 74 | Creatio | T: record changed; Q: get contact; M: create lead |
| 75 | Zendesk Sell | T: deal changed; Q: get lead; M: create deal |

## Tier D — finance, HR, data, and media breadth

| # | Connector candidate | Suggested first capabilities |
|---:|---|---|
| 76 | Odoo | T: record changed; Q: search records; M: create record |
| 77 | Oracle NetSuite | T: record changed; Q: get customer; M: create sales order |
| 78 | Sage Intacct | Q: get invoice; Q: list customers; M: create invoice |
| 79 | FreshBooks | T: invoice paid; Q: get invoice; M: create invoice |
| 80 | Wave | T: invoice paid; Q: get customer; M: create invoice |
| 81 | Braintree | T: transaction settled; Q: get transaction; M: create transaction |
| 82 | Adyen | T: payment event; Q: get payment; M: capture payment |
| 83 | Chargebee | T: subscription changed; Q: get subscription; M: create subscription |
| 84 | Recurly | T: subscription changed; Q: get account; M: create subscription |
| 85 | Paddle | T: transaction completed; Q: get transaction; M: create transaction |
| 86 | Razorpay | T: payment captured; Q: get payment; M: create payment link |
| 87 | Plaid | T: transaction sync; Q: get accounts; Q: sync transactions |
| 88 | Wise Platform | T: transfer status changed; Q: get transfer; M: create transfer |
| 89 | Gusto | T: employee changed; Q: get employee; M: create employee |
| 90 | Deel | T: contract changed; Q: get contract; M: create contract |
| 91 | Rippling | T: employee changed; Q: get employee; M: create employee |
| 92 | Personio | T: employee changed; Q: get employee; M: create employee |
| 93 | Workday | T: worker changed; Q: get worker; M: update worker |
| 94 | Greenhouse | T: candidate created; Q: get candidate; M: create candidate |
| 95 | Lever | T: opportunity changed; Q: get opportunity; M: create opportunity |
| 96 | Ashby | T: application changed; Q: get candidate; M: create candidate |
| 97 | OpenSearch | Q: search documents; M: index document; M: delete document |
| 98 | ClickHouse | Q: run query; M: insert rows; Q: list tables |
| 99 | Databricks | Q: run SQL statement; Q: get job run; M: start job |
| 100 | Amazon Redshift | Q: run SQL statement; Q: get statement result; Q: list tables |

## Implementation constraints for this backlog

Before selecting a row, confirm provider API availability, commercial access, OAuth scopes or credential model, event transport, stable IDs, rate limits, retry and idempotency semantics, and the exact first use case. For each new connector, implement and verify Go first, add its connector-local runnable Flow, then record conformance scenarios and port the selected capabilities to Python and TypeScript. A language package must list exactly its implemented capabilities. Do not treat the proposed Trigger in each row as an obligation if the provider cannot deliver it safely.

The 13 existing Trigger ports are a separate, nearer-term language project. They need Dex integration coverage of delivery and duplicate handling, plus language-specific examples and manifest support; merely adding names to `spec.languages` would not establish parity. The React primitives, Connector Studio UI, and directory site have no changes from this research document; connector implementation will require a fresh configuration UX review for each selected provider.

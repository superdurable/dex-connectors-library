# Postmark connector for TypeScript

Install `@superdurable/connector-postmark@0.1.0`. This package implements
`listOutboundMessages` and `getMessageDetails` as Dex Queries. The generated
Step factories are `newListOutboundMessagesStep` and
`newGetMessageDetailsStep`; `GeneratedMock` supplies manifest defaults.

The list Query accepts a `count` and `offset`. Count zero uses 50; count must
be at most 500 and the page must fit Postmark's 10,000-message search window.
The detail Query returns `notFound` for HTTP 404 or Postmark error code 701.
HTTP 408, 429, 5xx, and transport failures retry. Other HTTP 4xx responses
select `providerRejected`; malformed or oversized success bodies select
`invalidResponse`. The remaining branches are optional and fail an example
Flow when unwired.

Create a Dex Web connection named `postmark` using the
[authorization guide](../README.md#authorization). The Server API token is
resolved on each attempt and sent only in the provider header. No token is
part of the Flow input or retained result.

The runnable [outbound lookup example](examples/outbound-lookup/README.md)
starts a Worker and a Flow with that static connection name. Its integration
test uses a local provider stand-in and a real Dex Worker and Client. Run
`npm run check` for type, lint, unit, conformance replay, and build checks.
Live Postmark authorization and API behavior remain unverified.

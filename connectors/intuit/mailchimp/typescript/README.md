# Mailchimp TypeScript connector

`@superdurable/connector-intuit-mailchimp` provides typed Dex operations for reading and listing audience contacts, upserting contacts, updating tags, and sending a reviewed campaign.

Install with `npm install @superdurable/connector-intuit-mailchimp@0.1.0`. Configure a Mailchimp API key in Dex Web as described in the [connector README](../README.md#mailchimp-setup). The package derives the data center from the key suffix and never puts the key in Flow input or output.

The runnable [get-member example](examples/get-member/README.md) shows the static connection name, Worker, and Flow. Its integration test uses a local Mailchimp stand-in and a real `dexcli dev` server. The Go-recorded [conformance scenarios](../../../../conformance/connectors/intuit/mailchimp/scenarios.json) cover all five operations, including retries and campaign send recovery.

Live Mailchimp provider authorization and behavior have not been verified for this TypeScript package.

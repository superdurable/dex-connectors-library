# ActiveCampaign TypeScript connector

This package implements `listContacts` and `getContact` for Dex TypeScript Flows. Both operations replay the Go reference's provider requests, branches, retries, and exact outputs through shared conformance scenarios. Embedded relationships and custom field values are excluded from Flow results.

Sign in to [ActiveCampaign](https://www.activecampaign.com/login), open Settings > Developer, and copy the account API URL into `baseUrl` and the API Key into the Dex Web connection secret. The package sends the key only in the `Api-Token` header. The [connector README](../README.md) describes operation inputs and defaults; [contact inspection](examples/contact-inspection/README.md) is the runnable example.

Live ActiveCampaign authorization and responses have not been verified locally. Package release is independent of the Go and Python releases.

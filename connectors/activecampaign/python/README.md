# ActiveCampaign Python connector

The Python package implements `listContacts` and `getContact` with provider requests and branch decisions replayed against the Go reference. It returns contact ID, email address, and names without embedded relationships. Sign in to [ActiveCampaign](https://www.activecampaign.com/login), open Settings > Developer, and copy the account-specific API URL into `baseUrl` and your API Key into the Dex connection secret. See [the connector README](../README.md) and [the runnable example](examples/contact_inspection/README.md).

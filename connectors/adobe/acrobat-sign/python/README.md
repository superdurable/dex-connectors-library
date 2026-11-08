# Adobe Acrobat Sign Python connector

This package implements the `getAgreement` Query for Dex Python Flows. It reads only an agreement's ID, name, and status through Acrobat Sign REST API v6. The connector resolves and caches the account's regional API access point before lookup. Go-recorded conformance scenarios verify the request and branch contract.

Create an integration key with `agreement_read` scope and save it in the Dex Web connection's secret `integration_key` field. See the [connection guide](../README.md) and [runnable example](examples/agreement_inspection/README.md). Live Adobe authorization and lookup remain unverified.

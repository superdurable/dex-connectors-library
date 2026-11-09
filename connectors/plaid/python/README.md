# Plaid Python connector

Implements `getAccounts`, returning linked account IDs, types, and subtypes without names, masks, or balances. Go-recorded conformance checks exact requests, branches, retries, and outputs.

Copy the client ID and environment secret from the Plaid Dashboard **Build > Keys** into Dex Web `client_id` and `secret`. Exchange a Plaid Link public token and store the Item `access_token` in the same connection. Leave `baseUrl` blank for Sandbox or set `https://production.plaid.com` for Production. See the [connection guide](../README.md) and [runnable example](examples/accounts_inspection/README.md). Live provider authorization remains unverified.

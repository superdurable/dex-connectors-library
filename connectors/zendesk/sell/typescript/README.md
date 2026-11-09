# Zendesk Sell TypeScript connector

This package implements `listLeads` and `getLead` for Dex TypeScript Flows. It replays the Go reference's requests, branches, retries, and exact outputs through shared conformance scenarios. Flow results contain lead ID, name, email, and organization, omitting custom fields and links.

Sign in to [Zendesk Sell](https://app.futuresimple.com/) with account management privileges and open Integrations > OAuth > Access Tokens. Add a User Token, then copy its one-time secret into Dex `access_token`. The API root defaults to `https://api.getbase.com/v2`; leave `baseUrl` blank unless using a sanctioned proxy. See the [connector README](../README.md) and [runnable example](examples/lead-discovery/README.md). Live provider authorization remains unverified.

JavaScript numbers cannot represent provider numeric IDs above the safe integer limit exactly; such responses select `invalidResponse`. The TypeScript package releases independently of the Go and Python packages.

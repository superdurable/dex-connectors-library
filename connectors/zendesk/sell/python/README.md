# Zendesk Sell Python connector

This package implements `listLeads` and `getLead`. It replays the Go-recorded requests, branches, retry decisions, and typed outputs. Flow results contain lead ID, name, email, and organization, omitting custom fields and links.

Sign in to [Zendesk Sell](https://app.futuresimple.com/) with account management privileges and open Integrations > OAuth > Access Tokens. Add a User Token, then copy its one-time secret into Dex `access_token`. The API root defaults to `https://api.getbase.com/v2`; leave `baseUrl` blank unless using a sanctioned proxy. See the [connector README](../README.md) and [runnable example](examples/lead_discovery/README.md). Live provider authorization remains unverified.

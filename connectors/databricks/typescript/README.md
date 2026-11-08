# Databricks Typescript connector

Implements `getJobRun` with a typed job run ID, current state, and optional terminal result state. Go-recorded conformance covers exact requests, branches, retries, and outputs.

Create a workspace token under **Settings > Developer > Access Tokens > Manage > Generate new token** and save it as `api_token` in Dex Web Connections. Set required `baseUrl` to that workspace's HTTPS root. See the [connection guide](../README.md) and [runnable example](examples/job-run-inspection/README.md). Live provider authorization remains unverified.

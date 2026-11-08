# Twilio Segment Python connector

This package implements `getSource`, returning the Source ID, slug, and display name without write keys. Go-recorded conformance verifies requests, branches, retries, and output.

Create a Workspace Public API token with Sources read access in [Segment](https://app.segment.com) under **Settings > Access Management > Tokens > Create Token**. Enter the complete secret as `api_key` in Dex Web Connections. Blank `baseUrl` uses the US endpoint; use `https://eu1.api.segmentapis.com` for EU Workspaces. See the [connection guide](../README.md) and [runnable example](examples/source_inspection/README.md). Live provider authorization remains unverified.

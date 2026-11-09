# Miro Python connector

This package implements `getBoard` for Dex Python Flows. It reads only a board ID and name from Miro REST API v2. Go-recorded conformance scenarios verify exact requests, branches, retries, and outputs.

Create a Developer team app in [Miro Your apps](https://miro.com/app/settings/user-profile/apps), grant `boards:read`, install and authorize the app, and enter its opaque token in Dex Web secret `access_token`. Leave `baseUrl` blank for the public API endpoint. See the [connector README](../README.md) and [runnable example](examples/board_inspection/README.md). Real Miro authorization remains unverified.

# Shortcut TypeScript connector

This package implements `getStory` for Dex TypeScript Flows. It reads only the Story ID, title, type, workflow state ID, and archived flag from Shortcut REST API v3. Go-recorded conformance scenarios verify requests, branches, retries, and outputs.

Create an API token in [Shortcut Account Settings > API Tokens](https://app.shortcut.com/settings/account/api-tokens) and enter it in Dex Web secret `api_token`. Leave `baseUrl` blank for the public API endpoint. See the [connector README](../README.md) and [runnable example](examples/story-inspection/README.md). Real Shortcut authorization remains unverified.

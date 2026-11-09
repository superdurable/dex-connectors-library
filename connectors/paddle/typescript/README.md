# Paddle Typescript connector

This package implements `getTransaction`. It returns ID, status, and currency without customer or payment details. Go-recorded conformance checks exact requests, branches, retries, and outputs.

Create a secret server-side API key under **My account > Settings > Authentication > API keys** in Paddle and grant `transaction.read`. Save it as `api_key` in Dex Web Connections. Leave `baseUrl` blank for Sandbox or set `https://api.paddle.com` for Live. See the [connection guide](../README.md) and [runnable example](examples/transaction-inspection/README.md). Live provider authorization remains unverified.

# Chargebee Python connector

Implements `getSubscription`, returning the subscription ID, status, and currency code without customer or payment details. Go-recorded conformance checks exact requests, branches, retries, and outputs.

Create a Read-Only API key in the Chargebee site's **Settings > Configure Chargebee > API Keys and Webhooks > API Keys** and save it as `api_key` in Dex Web Connections. Set `baseUrl` to the matching `https://{site}.chargebee.com/api/v2`. See the [connection guide](../README.md) and [runnable example](examples/subscription_inspection/README.md). Live provider authorization remains unverified.

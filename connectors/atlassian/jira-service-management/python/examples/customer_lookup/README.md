# Jira Service Management customer lookup Python example

Create a Dex Web connection named `jsm-support` with the Atlassian OAuth application described in the connector README. Choose the Jira site and service desk through the connection UI. Start `dexcli dev`, set `DEX_FLOW_SERVICE_ADDRESS` to its Flow service address, and run `python -m examples.customer_lookup.main` from this Python package. Start `JiraServiceManagementCustomerLookupPython` with `{"service_desk_id":"10","email":"jane@example.com"}`. The Flow returns matching customer account IDs and names; an unwired `notFound` branch fails the Flow.

The integration test runs a real Dex Worker and Client with a local Jira stand-in. Live Atlassian OAuth consent and provider behavior remain unverified.

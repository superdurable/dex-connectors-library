# Support request customer lookup

Create a Dex Web connection named `jira-service-management-support` using the Atlassian OAuth setup in the connector README. Choose a Jira Service Management site and set its `cloudId` in the connection. Run `python -m examples.support_request.main` from this package with `DEX_FLOW_SERVICE_ADDRESS` set to your `dexcli dev` address. Start Flow `JiraSupportRequestLookupPython` with `{"serviceDeskId":"10","email":"jane@example.com"}` or `JiraCreateSupportTicketPython` with `{"serviceDeskId":"10","requestTypeId":"25","summary":"Laptop will not boot","description":"My laptop will not boot."}`.

The lookup Flow stores customer account IDs and display names; the creation Flow stores the new ticket reference. Unwired optional branches fail their Flow. The integration test uses a local provider stand-in and a real Dex Worker and Client, including a definite rate limit before request creation; live Atlassian OAuth and provider behavior remain unverified.

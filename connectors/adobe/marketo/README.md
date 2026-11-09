# Adobe Marketo Engage connector

The Go, Python, and TypeScript packages read selected lead profile fields through the Marketo REST API. `getLead` takes a positive lead ID and returns its ID, first name, and last name. It omits email and all other fields. A successful response with no result selects `notFound`; Marketo response codes for rate limits, temporary failures, or an expired access token (602) ask Dex to retry.

## Connection

In Dex Web **Connections**, create `adobe-marketo` using the authorization guide in `connector.yaml`. In Marketo, create an API-Only user with an API role granting Access API > Read-Only Lead, then create a Custom service in Admin > Integration > LaunchPoint. Enter its Client ID and secret into Dex Web. Under Admin > Integration > Web Services, copy the subscription REST Endpoint into `restEndpoint` and the Identity URL into `tokenEndpoint` with `/oauth/token` appended. Both URLs must use the same HTTPS `.mktorest.com` host. Leave `access_token`, `timeout`, and `maxResponseBytes` blank. Dex mints and refreshes the access token using the custom service.

The [runnable Go](go/examples/lead-inspection/README.md), [Python](python/examples/lead_inspection/README.md), and [TypeScript](typescript/examples/lead-inspection/README.md) Flows use this static connection name. Optional branches are unwired and fail the Flow when selected. The real Marketo authorization form and provider read have not been verified live.

This operation follows Adobe's [Lead API](https://experienceleague.adobe.com/en/docs/marketo-developer/marketo/rest/lead-database/leads), [authentication](https://experienceleague.adobe.com/en/docs/marketo-developer/marketo/rest/authentication), and [error code](https://experienceleague.adobe.com/en/docs/marketo-developer/marketo/rest/error-codes) contracts.

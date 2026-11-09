# Oracle NetSuite connector

The `getCustomer` Query reads a customer through [NetSuite REST Web Services](https://docs.oracle.com/en/cloud/saas/netsuite/ns-online-help/article_161581923822.html). It accepts a numeric internal ID and returns the internal ID and the `entityId` shown by NetSuite. The Go package is at [go/](go/).

Configure an OAuth 2.0 integration with the `rest_webservices` scope and customer read permission. Register the exact Dex Web redirect URI. Copy the account-specific REST, authorization, and token URLs from the account ID; [Oracle's OAuth setup tutorial](https://docs.oracle.com/en/cloud/saas/netsuite/ns-online-help/section_0602033701.html) documents their formats. NetSuite uses Basic client authentication for token exchange. Keep the client secret and tokens in Dex credentials.

HTTP 408, 429, 5xx, and transport failures retry. A missing customer, provider rejection, malformed response, or local configuration defect selects an optional branch. The [runnable Go example](go/examples/customer-inspection/README.md) wires only the happy path and includes a real Dex mock integration test. Live OAuth consent and customer reads require a user-controlled NetSuite account and have not been verified.

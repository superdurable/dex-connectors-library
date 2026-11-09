# Microsoft Advertising connector

This connector lists campaigns in an account through Microsoft's [Campaign Management v13 REST API](https://learn.microsoft.com/en-us/advertising/campaign-management-service/getcampaignsbyaccountid?view=bingads-13). Version `v0.1.0` provides the Go `listCampaigns` Query; Python and TypeScript ports are pending.

`listCampaigns` takes a numeric account ID and returns each campaign's ID, name, type, and status. It requests all currently documented campaign types explicitly because the API otherwise defaults to Search. An empty list is a successful result. HTTP 408, 429, 5xx, and transport failures retry; rejected requests and malformed responses select optional branches. The example leaves those branches unwired, so they fail the Flow.

Register a Web application in [Microsoft Entra](https://entra.microsoft.com/) with the exact Dex Web redirect URI and the Microsoft Advertising `msads.manage` delegated permission. Dex requests `offline_access` and refreshes tokens. Obtain a developer token from the [Microsoft Advertising Developer Portal](https://developers.ads.microsoft.com/). In the signed-in Campaigns URL, `cid` is the customer ID for the connection and `aid` is the account ID passed to the Flow. Keep the client secret and developer token in Dex credentials, never Flow input or Results.

The runnable [Go campaign discovery example](go/examples/campaign-discovery/README.md) demonstrates the Query and real Dex mock integration. Live authorization and campaign reads have not been verified without a user-controlled advertising account.

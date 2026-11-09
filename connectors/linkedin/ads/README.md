# LinkedIn Ads connector

The Go, Python, and TypeScript `listAdAccounts` Queries read one cursor-based page of LinkedIn Advertising accounts visible to an authorized member. Each result contains a decimal account ID, display name, and optional next-page token. The operation uses the [LinkedIn Ad Accounts API](https://learn.microsoft.com/en-us/linkedin/marketing/integrations/ads/account-structure/create-and-manage-accounts?tabs=http&view=li-lms-2026-05) and the `r_ads` OAuth scope. The runnable [Go](go/examples/accounts/README.md), [Python](python/examples/accounts/README.md), and [TypeScript](typescript/examples/accounts/README.md) Flows use the static `linkedin-ads` Dex Web connection.

Create or select an app in [LinkedIn Developer](https://www.linkedin.com/developers/apps), request Advertising API access under **Products**, and confirm `r_ads` appears under **Auth**. Register the redirect URL shown by Dex Web and copy the app's Client ID and Client Secret into the connection form. The member granting consent needs an appropriate Ad Account role. LinkedIn may require separate approval for programmatic refresh tokens; without one, reauthorize when the access token expires. See the [LinkedIn Marketing access guide](https://learn.microsoft.com/en-us/linkedin/marketing/increasing-access?view=li-lms-2026-09).

The API sends `Linkedin-Version: 202609` and `X-Restli-Protocol-Version: 2.0.0`. `pageSize` accepts 1–1000 and defaults to 100. Pass `nextPageToken` as the next request's `pageToken`; a blank token means there is no next page. Campaign reporting and mutations are outside this first release.

Live LinkedIn consent, Advertising API approval, token refresh, provider reads, and Dex Web field rendering have not been verified in this checkout.

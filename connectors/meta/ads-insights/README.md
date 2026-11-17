# Meta Ads Insights connector

`getAd` reads a Meta Marketing API ad's ID, name, configured status, and effective delivery status. Go's `getInsights` reads a cursor page of account, campaign, ad set, or ad performance metrics: impressions, clicks, and spend in the ad account currency. Both require a Meta Business app with Marketing API access, an ad account role, and an access token granted `ads_read`. Meta may require Advanced Access and App Review when reading other businesses' accounts.

In [Meta for Developers](https://developers.facebook.com/apps/), select the Business app and add Marketing API. Generate a User token in [Graph API Explorer](https://developers.facebook.com/tools/explorer/) or a System User token in Meta Business Settings > Users > System users. Store the opaque token only in Dex Web Connections. Find the numeric ad ID in Ads Manager > Campaigns > Ads and pass it as `adId`.

The runnable [Go](go/examples/ad-inspection/README.md), [Python](python/examples/ad_inspection/README.md), and [TypeScript](typescript/examples/ad-inspection/README.md) ad inspection Flows call `getAd` with `{"adId":"123456789012345"}`. The Go example also has an insights report Flow. Go-recorded provider scenarios check Python and TypeScript request and outcome parity for their declared operation. Mock backed real Dex tests verify the typed outputs. Live Meta authorization and insights metrics remain unverified.

Meta's [official Marketing API collection](https://www.postman.com/meta/facebook-marketing-api/documentation/0zr4mes/facebook-marketing-api-mapi) documents ad retrieval and `ads_read` access.

Meta's [Ads Insights API](https://developers.facebook.com/documentation/ads-commerce/marketing-api/insights) documents the four object insights edges and the `ads_read` permission. Supply a numeric object ID or an `act_` ad account ID. `datePreset` is an optional Meta date preset such as `last_7d`; blank uses Meta's reporting window. `limit` is a row count from 1 to 500; zero uses Meta's default. Pass `nextCursor` as `after` for the next page; blank starts over.

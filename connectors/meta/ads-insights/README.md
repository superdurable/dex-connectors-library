# Meta Ads Insights connector

`getAd` reads a Meta Marketing API ad's ID, name, configured status, and effective delivery status. It requires a Meta Business app with Marketing API access, an ad account role, and an access token granted `ads_read`. Meta may require Advanced Access and App Review when reading other businesses' accounts.

In [Meta for Developers](https://developers.facebook.com/apps/), select the Business app and add Marketing API. Generate a User token in [Graph API Explorer](https://developers.facebook.com/tools/explorer/) or a System User token in Meta Business Settings > Users > System users. Store the opaque token only in Dex Web Connections. Find the numeric ad ID in Ads Manager > Campaigns > Ads and pass it as `adId`.

The runnable [Go](go/examples/ad-inspection/README.md), [Python](python/examples/ad_inspection/README.md), and [TypeScript](typescript/examples/ad-inspection/README.md) ad inspection Flows call `getAd` with `{"adId":"123456789012345"}`. Go-recorded provider scenarios check Python and TypeScript request and outcome parity. Mock backed real Dex tests verify all three typed outputs. Live Meta authorization remains unverified.

Meta's [official Marketing API collection](https://www.postman.com/meta/facebook-marketing-api/documentation/0zr4mes/facebook-marketing-api-mapi) documents ad retrieval and `ads_read` access.

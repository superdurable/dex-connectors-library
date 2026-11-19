# Google Search Console connector

The Go, [Python](python/README.md), and [TypeScript](typescript/README.md) connectors list properties accessible to the connected Google account, returning each property URL and permission level. The Go connector also provides `getSite`, `querySearchAnalytics`, `listSitemaps`, `getSitemap`, and `inspectURL` as safe Queries under the existing read-only OAuth scope. The runnable [Go sites Flows](go/examples/sites/README.md) use the static `google-search-console` Dex Web connection. Python and TypeScript continue to implement only `listSites` and replay its Go-recorded scenarios.

## Connection setup

In [Google Cloud Console](https://console.cloud.google.com/apis/credentials), select a project and enable Search Console API under **APIs & Services → Library**. Configure the OAuth consent screen, add the `webmasters.readonly` scope, and create a Web application under **Clients**. Register the redirect URI displayed by Dex Web. Save the Client ID and Client Secret in the Dex Web connection, then authorize a Google account that can access the desired Search Console properties. Google consent must include offline access so Dex can renew the token.

The API base URL, request timeout, and response limit have manifest defaults. Override them only for a trusted gateway or operational need. Access and refresh tokens remain Dex secrets and must not enter Flow input or results.

[Google's API reference](https://developers.google.com/webmaster-tools/v1/api_reference_index) defines the property, search analytics, and sitemap endpoints; [URL Inspection](https://developers.google.com/webmaster-tools/v1/urlInspection.index/inspect) uses a separate Google API origin. Search Analytics dates use inclusive Pacific calendar days; `rowLimit` is 1–25,000 or zero for Google's default. URL Inspection reports the version in Google's index, not a live fetch. Site add/delete and sitemap submit/delete require the broader `webmasters` scope and are intentionally unavailable under existing connections. Live Google authorization has not been verified in this checkout.

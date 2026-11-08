# Google Search Console connector

The Go and [Python](python/README.md) connectors list properties accessible to the connected Google account, returning each property URL and permission level. Their runnable sites Flows use the static `google-search-console` Dex Web connection and the `listSites` Step. Python replays the Go-recorded provider scenarios.

## Connection setup

In [Google Cloud Console](https://console.cloud.google.com/apis/credentials), select a project and enable Search Console API under **APIs & Services → Library**. Configure the OAuth consent screen, add the `webmasters.readonly` scope, and create a Web application under **Clients**. Register the redirect URI displayed by Dex Web. Save the Client ID and Client Secret in the Dex Web connection, then authorize a Google account that can access the desired Search Console properties. Google consent must include offline access so Dex can renew the token.

The API base URL, request timeout, and response limit have manifest defaults. Override them only for a trusted gateway or operational need. Access and refresh tokens remain Dex secrets and must not enter Flow input or results.

[Google's `sites.list` reference](https://developers.google.com/webmaster-tools/v1/sites/list) defines `GET /webmasters/v3/sites` and the `siteEntry` response. This first release implements that read only; Search Analytics and URL Inspection remain future capabilities. Live Google authorization has not been verified in this checkout.

# Kuaishou connector

The Go `listUserVideos` Query reads one page of videos belonging to the authorized Kuaishou account through the [official Open Platform endpoint](https://open.kuaishou.com/platformDocs/openAbility/contentManagement/searchForVideos). The connector returns video IDs, captions, creation timestamps, engagement counts, and processing status. It omits cover and playback URLs. The returned `nextCursor` is the last photo ID; another request with that cursor fetches the next page, which may be empty.

## Connect

At the [Kuaishou Open Platform](https://open.kuaishou.com), open **控制台 → 应用管理**, create or select an application, and copy `app_id`. Request the `user_video_info` scope and have the account owner approve it. Enter the resulting OAuth2 access token into Dex Web Connections as a secret. The [official login guide](https://open.kuaishou.com/platformDocs/openAbility/kwaiLogin) says access tokens typically expire after 48 hours; the application owner must refresh it through Kuaishou's OAuth2 endpoint and update the Dex credential. The app secret remains with the application owner and is not used by this list query. Leave transport and page-size settings blank for their displayed defaults.

Kuaishou documents `app_id` and `access_token` as HTTPS query parameters. The connector never places the token in a Flow input, output, error, or log; its HTTP client does not follow redirects. HTTP 429, HTTP 5xx, and transport failures retry. Definite provider errors select optional `providerRejected`; malformed or oversized responses select optional `invalidResponse`. The example leaves optional branches unwired, so those outcomes fail the Flow. The official list-endpoint documentation does not publish a fixed quota; check current limits in the portal.

The runnable Flow is in [`go/examples/user-videos`](go/examples/user-videos). Provider tests verify request shape and outcome classification. Live `user_video_info` approval, OAuth2 authorization, and production videos remain unverified without user-controlled credentials.

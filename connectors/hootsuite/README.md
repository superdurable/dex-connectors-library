# Hootsuite connector

The Go connector lists social profiles visible to the connected Hootsuite user. It returns each profile's Hootsuite ID, network type, and username; the operation takes no input. The [runnable Flow](go/examples/social-profiles/README.md) uses the static `hootsuite` connection and the generated `NewListSocialProfilesStep` factory.

## Connection setup

Create or select an app in the [Hootsuite Developer Portal](https://developer.hootsuite.com/) under **My Apps**. REST API access may require Hootsuite approval. Register the redirect URI displayed by Dex Web, then enter the app's Client ID and Client Secret in the Dex Web connection. Authorize with the `offline` scope so Dex can refresh the token. The authorizing Hootsuite user must be able to access the desired social profiles.

The API base URL, request timeout, and response size limit have manifest defaults. Only override them for a trusted gateway or a known operational need. Dex stores access and refresh tokens as secrets; do not put them in Flow inputs or results.

The [official Hootsuite API documentation](https://apidocs.hootsuite.com/docs/api/index.html) describes the `GET /v1/socialProfiles` request and OAuth2 setup. The default mock returns one example Facebook page. Provider authorization and live API behavior have not been verified in this checkout.

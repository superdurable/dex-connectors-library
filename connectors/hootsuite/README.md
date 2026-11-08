# Hootsuite connector

The Go connector lists social profiles, reads scheduled posts, and schedules text posts. The [social profiles Flow](go/examples/social-profiles/README.md), [scheduled posts Flow](go/examples/scheduled-posts/README.md), and [post scheduling Flow](go/examples/post-scheduling/README.md) use the static `hootsuite` connection and operation-specific Step factories.

The [Python package](python/README.md) implements the same three operations and includes a runnable [social profiles Flow](python/examples/social_profiles/README.md). Both languages are checked against the Go-recorded provider scenarios.

## Connection setup

Create or select an app in the [Hootsuite Developer Portal](https://developer.hootsuite.com/) under **My Apps**. REST API access may require Hootsuite approval. Register the redirect URI displayed by Dex Web, then enter the app's Client ID and Client Secret in the Dex Web connection. Authorize with the `offline` scope so Dex can refresh the token. The authorizing Hootsuite user must be able to access the desired social profiles.

The API base URL, request timeout, and response size limit have manifest defaults. Only override them for a trusted gateway or a known operational need. Dex stores access and refresh tokens as secrets; do not put them in Flow inputs or results.

The [official Hootsuite API documentation](https://apidocs.hootsuite.com/docs/api/index.html) describes `GET /v1/socialProfiles`, `GET /v1/messages`, `POST /v1/messages`, and OAuth2 setup. The scheduled-post query accepts a UTC interval of at most four weeks and returns one page with a continuation cursor. The scheduling Mutation uses a single-dispatch fence because Hootsuite offers no request idempotency key; an ambiguous outcome is `uncertain` and requires inspection in Planner before another post is sent. Provider authorization and live API behavior have not been verified in this checkout.

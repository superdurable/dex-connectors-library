## Summary

<!-- What changed, and why? -->

## Verification

<!-- List automated tests and manual checks. -->

## Optional OAuth/OIDC authorization testing

This section is optional and must not block opening or early review of a pull
request. After opening a pull request that adds a connector or operation whose
manifest sets `spec.auth.type: oauth2`, authors are encouraged to test the real
provider authorization configuration and share a screenshot. API keys and other
static credentials do not need this testing recommendation.

Connector/operation:

Authorization testing status:

Screenshot or attachment: <!-- Optional; add it to the PR description. -->

Notes: <!-- State plainly when live authorization testing is incomplete. -->

Do not commit the screenshot to the repository. Redact tokens, client secrets,
account details, and unrelated personal information before attaching it.

Reviewers should treat this evidence as advisory and must not block early
connector sharing solely because it is absent.

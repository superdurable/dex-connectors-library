## Summary

<!-- What changed, and why? -->

## Verification

Checked-in example path: <!-- Required for a new connector, user-visible capability, or behavioral fix. Otherwise write N/A with a reason. -->

Dex Run evidence: <!-- Non-secret Run ID, terminal branch, and observed result or bounded side effect. -->

Real provider exercised: <!-- Yes or no. -->

Unverified live behavior: <!-- Write None, or name the exact behavior that was not safely verified. -->

Configuration surface evidence: <!-- Provider links/paths and affected authorization, operation, Trigger, and UI fields audited in Dex Web Connections. -->

Unverified configuration behavior: <!-- Write None, or name the exact untested page, scope, claim, picker, default, or blank behavior. -->

Test commands and results:

<!-- List every automated test and manual check with its result. -->

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

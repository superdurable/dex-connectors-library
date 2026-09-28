# Connector configuration guidance

Treat configuration as part of the connector contract. A user must be able to
open Dex Web **Connections**, follow provider-owned links, and finish setup
without guessing where a value comes from or transcribing a provider constant.

## Audit the complete rendered surface

For every runnable example, enumerate the authorization form and every visible
field from each operation and Trigger `ConfigurationUI`. Audit the rendered Dex
Web setup page, not only `connector.yaml` or a Studio component in isolation. A
field label or generic type description is not guidance.

Keep provider constants and safe operational defaults in `connector.yaml`.
Users must not enter API endpoints, scopes, fixed limits, or other values the
connector already knows. Dex Web displays each manifest default below its field
as parenthetical guidance. The field description explains units, valid format,
blank behavior, and when an override is useful without repeating the literal
default.

## Derive values before explaining them

Do not keep a free-text field when the provider can supply the value safely.
Derive identity fields from verified OAuth or OpenID Connect claims. Derive
resource identifiers through a declared read-only setup command and a
release-owned picker. The connector validates the response and saves the stable
provider ID while displaying a human-readable label.

The Studio iframe never receives credentials. A setup command declares its
HTTPS destination, credential mapping, request parameters, response bound, and
read-only backend capability in `connector.yaml`; the host injects the secret
and rejects secret reflection. Keep manual-ID fallback only when the provider
cannot enumerate the resource or the documented API requires it.

## Authorization guide

Every credentialed connector declares `spec.auth.guide.startURL` and ordered
`steps`. Start at the provider page where setup actually begins, then name the
exact navigation path and action. Static credentials explain how to create,
restrict, reveal, and revoke each key or signing secret, including its expected
prefix or format and whether test and production modes must match.

For OAuth 2.0 or OpenID Connect, explain all preparation before authorization:

- the provider application or cloud-project page;
- the exact redirect URI shown by Dex Web and where to register it;
- APIs or products to enable;
- requested scopes and why they are needed;
- consent-screen, audience, test-user, publishing, or administrator approval;
- where the client ID and one-time client secret are created and copied.

Access tokens and derived identity claims are outputs of authorization, not
manual inputs. Mark them accordingly in their manifest descriptions and never
ask the user to paste a value the OAuth exchange or a verified claim produces.

## Every remaining field

For each user-supplied authorization, operation, or Trigger field, the visible
guidance states all of the following:

1. The provider HTTPS URL where the user starts.
2. The exact provider page, menu path, or API resource.
3. How to create, select, or find the value.
4. The expected format and units, with a non-secret shape example when useful.
5. Whether the value is secret and how it is stored.
6. What leaving the field blank means.

Put credential-wide instructions in the authorization guide. Put field-specific
instructions in the manifest field description or release-owned UI unit. For a
composed operation or Trigger UI, `ConnectorUIUnit.Description` is required and
explains the choice in that operation or Trigger's context, including what a
picker writes and what blank means. Do not rely on a generic unit-catalog
description as the only copy shown to the user.

## Verification

Configuration tests enumerate every visible authorization, operation, and
Trigger field exposed by every runnable example. Cover instructional links,
derived claims and picker outputs, parenthesized defaults, validation, blank
semantics, and secret-safe rendering. Assert that secrets never appear in DOM,
iframe messages, logs, snapshots, generated values, catalogs, or artifacts.

Run the complete connector verification matrix, then use the exact connector
release metadata in Dex Web **Connections**. Confirm each link and provider
path, authorize or save the connection, configure every example operation and
Trigger, restart the application where required, and verify the saved values
take effect. If live provider verification is unavailable, name the exact
untested page, scope, claim, or picker behavior in the pull request.

# Connector configuration UX

- Every connector has at least one runnable Flow under `examples/` with a
  Worker entrypoint, strict FDG 2.0 generation instructions, deterministic
  tests, and the connector's static `ConnectionName`.
- Audit the authorization form and every operation and Trigger
  `ConfigurationUI` unit exposed by every runnable example. Every visible
  field has connector-owned guidance; a label or generic type description is
  insufficient.
- Keep provider constants and safe operational defaults in `connector.yaml`.
  Do not make users transcribe endpoints, limits, scopes, or other fixed values.
- Dex Web displays each manifest default below its field as parenthetical
  guidance. Describe its units, valid format, and when an override is useful;
  do not duplicate the literal default in prose.
- Derive identity and resource fields from verified OAuth/OIDC claims or
  declared read-only setup commands whenever the provider can supply them.
  Do not replace derivation with explanatory copy or duplicate free-text input.
- Every credentialed connector declares an authorization guide. For every
  remaining user-supplied field in authorization, operation, or Trigger UI,
  state the provider URL to start from, the exact page path, how to create,
  select, or find the value, its expected format and units, whether it is
  secret, and what blank means.
- OAuth/OIDC setup identifies the provider application page, redirect URI,
  enabled APIs, scopes, and consent requirements before starting authorization.
- `ConnectorUIUnit.Description` is required and explains the choice in the
  context of that operation or Trigger, including provider-derived picker
  output and blank behavior.
- Configuration UI tests enumerate visible fields and cover instructional
  links, derived outputs, parenthesized defaults, validation, and secret-safe
  rendering.

Cursor mirror: `.cursor/rules/connector-configuration-ux.mdc`.

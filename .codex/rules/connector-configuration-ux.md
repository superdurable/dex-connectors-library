# Connector configuration UX

- Every connector has at least one runnable Flow under `examples/` with a
  Worker entrypoint, strict FDG 2.0 generation instructions, deterministic
  tests, and the connector's static `ConnectionName`.
- Keep provider constants and safe operational defaults in `connector.yaml`.
  Do not make users transcribe endpoints, limits, scopes, or other fixed values.
- Derive identity and resource fields from verified OAuth/OIDC claims or
  declared read-only setup commands whenever the provider can supply them.
  Do not replace derivation with explanatory copy or duplicate free-text input.
- For every remaining user-supplied field, the authorization UI states the
  provider URL to start from, the exact page path, how to create or find the
  value, its expected format, and whether it is secret.
- OAuth/OIDC setup identifies the provider application page, redirect URI,
  enabled APIs, scopes, and consent requirements before starting authorization.
- Configuration UI tests cover instructional links, derived outputs, defaulted
  values, validation, and secret-safe rendering.

Cursor mirror: `.cursor/rules/connector-configuration-ux.mdc`.

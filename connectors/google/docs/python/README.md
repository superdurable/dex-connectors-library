# Google Docs Python connector

Dex-native Google Docs operations, with operation-specific Step factories for
`getDocumentText`, `createDocument`, `replaceDocumentText`, and `appendText`.
The package requires Connector SDK 0.7.0. Use `new_project_connection` for the
generated Google OAuth or Workspace domain delegation credential refresh wiring.
Provider creation timestamps must be valid Go JSON timestamps; invalid values
fail before a document Result is stored.

The [policy read example](examples/policy_read/README.md) shows the static
connection name, FDG generation, Worker, and Flow starter. See the
[parent connector README](../README.md) for authorization and operation guidance.

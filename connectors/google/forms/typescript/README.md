# Google Forms TypeScript connector

`@superdurable/connector-google-forms` exposes `getForm`, `listResponses`, and `getResponse` as Dex Query Steps. The package version is independent of the Go connector version and requires `@superdurable/connectors-sdk` 0.5.0.

Use the generated `newGetFormStep`, `newListResponsesStep`, and `newGetResponseStep` factories with the [runnable response recorder](examples/response-recorder/README.md). Its Worker loads `google-forms-reader` from the Dex project through `newProjectConnection`; the connection selects Google OAuth or Workspace domain delegation and refreshes access tokens through the manifest generated refresh driver. Keep credentials out of Flow inputs and Attributes.

The connector's [authorization guide](../README.md) covers Google Cloud application setup, both enabled APIs, scopes, redirect URI, consent, service account delegation, and configuration defaults. The `formId` can come from the Dex Web form picker. `listResponses` returns one bounded page and supports `submittedAfter`, `submittedAtOrAfter`, `pageSize`, and `pageToken`; read every page because Google does not document response order.

Generate the checked-in contracts after manifest or Go model changes from the repository root:

```sh
go run ./cmd/connectorctl generate connectors/google/forms/connector.yaml
```

Run the package, Go refresh agreement, and connector conformance tests:

```sh
npm run check --prefix connectors/google/forms/typescript
(cd connectors/google/forms/go && GOWORK=off go test ./... -run '^TestCredentialRefreshDriverAgreesWithTheManifest$' -count=1)
make test-connector-conformance CONNECTOR_DIRECTORY=connectors/google/forms
```

Live Google authorization, token refresh, Forms reads, and Drive form picker behavior remain unverified.

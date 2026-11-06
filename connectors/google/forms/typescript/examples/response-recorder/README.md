# Google Forms TypeScript response recorder

This Flow uses the static `google-forms-reader` connection. It reads a form, lists one response page, then reads the first response on that page. Only the `found` and `listed` branches are wired; other branches fail the Flow. The three provider outputs are persisted as Attributes.

Configure the Google OAuth or Workspace domain delegation connection in Dex Web using the [connector guide](../../../README.md). Use a form with at least one response. The input JSON has `formId`; the Flow reads the first listed response to demonstrate all three operations.

From the repository root, install dependencies and start Dex:

```sh
npm ci --prefix connectors/google/forms/typescript
dexcli dev -dex-port 41801 -web-port 41802 -open=false
```

In another terminal, point the Worker and client at the same Dex server:

```sh
export DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:41801
npm run example:list --prefix connectors/google/forms/typescript
npm run example:list:start --prefix connectors/google/forms/typescript -- connectors/google/forms/typescript/examples/response-recorder/input.example.json
```

The real Dex integration test uses a local provider stand-in with an OS-assigned port:

```sh
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:41801 npm run test:integration --prefix connectors/google/forms/typescript
```

Live Google authorization, Forms reads, and Drive picker behavior have not been verified by this test.

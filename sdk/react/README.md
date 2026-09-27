# `@superdurable/dex-connectors-react`

This package holds the shared pieces of Connector Studio bundles: connection
state, the Studio Host API client, Dex Web styling, and a live model picker.
The browser receives a logical provider name, a normalized status, and
provider responses the host has already checked for credential material;
secret values and OAuth tokens remain on the server.

```tsx
<ConnectionStatus
  provider="Google Sheets"
  state="expired"
  onReconnect={() => beginOAuth()}
/>
```

The package also defines Connector Studio Host API 0.2 for sandboxed bundles.
The host selects a `connection` surface or one `configurationUnit` surface.
Unit targets contain the Flow-owned instance identity, operation or Trigger
scope, port bindings, and only that unit's current values. A bundle saves port
values with `use.configuration.save`; Dex Web applies the declared bindings to
the isolated use configuration. Bundles call
`observeConnectorStudioFrameAutoHeight(hostReady)` from their UI lifecycle so a
`ResizeObserver` sends bounded `connector.frame.resize` messages whenever the
selected surface changes height. The host can then fit the sandbox iframe
without an inner scrollbar.

Every message carries the connector ID, protocol version, and session nonce.
Command messages additionally carry request identity. Hosts also validate the
iframe `Window` source and declared backend capability before executing a
command. Messages never carry provider credentials.

## Studio client and styling

`useConnectorStudioClient(connectorId)` waits for the host's ready message and
returns `send` and `executeProviderCommand`. Results are matched by request ID
and session nonce, and a rejected command raises
`ConnectorStudioCommandError` with the host's error code.
`collectProviderPages` follows provider cursors with a page cap, stops on a
repeated cursor, and reports `isTruncated`.

`applyConnectorStudioTheme(hostReady)` installs `connectorStudioStyles`, which
mirror the Dex Web v2 colour, type, and radius tokens, so a sandboxed bundle
matches the Connections page. It uses Dex Web's default light theme unless the
host reports `theme: "dark"`. `StudioSurface`, `StudioHeader`, `StudioField`,
`StudioButton`, and `StudioNotice` render the shared card, heading, labelled
control, CTA-green button, and inline status.

## Live model picker

`ModelPicker` is the configuration unit that LLM connectors share. The
connector declares a read-only `listModels` provider command and passes a
`loadModels` function that runs it through the host broker and projects the
provider's JSON into `ModelOption` values. Because the list is live, a model
the provider adds appears without a connector release. Models the connector's
filter judges unsuitable, such as embedding models, stay behind "Show all
models". The user can also keep the connection's default model, saved as an
empty `model`, or type any model ID when listing fails.

```tsx
<ModelPicker
  target={hostReady.target}
  providerName="Gemini"
  loadModels={loadGeminiModels}
  onSave={(value) => client.send("use.configuration.save", "use.configuration.write", {value})}
/>
```

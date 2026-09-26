# `@superdurable/dex-connectors-react`

This package renders connector connection state only. The browser receives a
logical provider name and a normalized status; secret values and OAuth tokens
remain on the server.

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

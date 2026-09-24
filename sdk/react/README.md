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

The package also defines the versioned Connector Studio Host API used by
sandboxed setup bundles. Every message carries the connector ID, protocol
version, session nonce, and request identity. Hosts must additionally validate
the iframe `Window` source and declared backend capability before executing a
command. Messages never carry provider credentials.

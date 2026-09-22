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

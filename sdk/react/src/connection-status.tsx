import type { ReactElement } from "react";

export type ConnectionState =
  | "not_configured"
  | "connecting"
  | "connected"
  | "expired"
  | "revoked"
  | "insufficient_scope"
  | "error";

export interface ConnectionStatusProps {
  provider: string;
  state: ConnectionState;
  detail?: string;
  onConnect?: () => void;
  onReconnect?: () => void;
}

const labels: Record<ConnectionState, string> = {
  not_configured: "Not configured",
  connecting: "Connecting",
  connected: "Connected",
  expired: "Connection expired",
  revoked: "Connection revoked",
  insufficient_scope: "Additional permission required",
  error: "Connection error",
};

export function ConnectionStatus({
  provider,
  state,
  detail,
  onConnect,
  onReconnect,
}: ConnectionStatusProps): ReactElement {
  const needsReconnect = state === "expired" || state === "revoked" || state === "insufficient_scope";
  const action = state === "not_configured"
    ? onConnect && <button type="button" onClick={onConnect}>Connect</button>
    : needsReconnect && onReconnect && <button type="button" onClick={onReconnect}>Reconnect</button>;

  return (
    <section aria-label={`${provider} connection`} data-connection-state={state}>
      <strong>{provider}</strong>
      <span role="status">{labels[state]}</span>
      {detail && <p>{detail}</p>}
      {action}
    </section>
  );
}

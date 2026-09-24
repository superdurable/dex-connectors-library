import { ConnectionStatus, type ConnectorConnectionView, type ConnectorStudioCommand } from "@superdurable/dex-connectors-react";

export interface GmailSetupViewProps {
  connection: ConnectorConnectionView;
  busy?: boolean;
  onCommand(command: ConnectorStudioCommand["command"]): void;
}

export function GmailSetupView({ connection, onCommand }: GmailSetupViewProps) {
  return <main className="card">
    <header><img src="./icon.svg" alt=""/><div><h1>Gmail</h1><p>Send follow-up email from the authorized primary address.</p></div></header>
    <ConnectionStatus
      provider="Gmail"
      state={connection.state === "authorization_pending" || connection.state === "broker_unavailable" ? "error" : connection.state}
      detail={connection.accountEmail ?? connection.detail}
      onConnect={() => onCommand("oauth.connect")}
      onReconnect={() => onCommand("oauth.reconnect")}
    />
    {connection.state === "connected" && <section className="controls">
      <p><strong>Primary sender:</strong> {connection.accountEmail}</p>
      <p className="note">Aliases are not available in alpha.</p>
    </section>}
  </main>;
}

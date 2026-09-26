import { ConnectionStatus, type ConnectorConnectionView } from "@superdurable/dex-connectors-react";

export interface GmailSetupViewProps { connection: ConnectorConnectionView; onConnect(): void; onReconnect(): void; }

export function GmailSetupView({connection, onConnect, onReconnect}: GmailSetupViewProps) {
  return <main className="card">
    <header><img src="./icon.svg" alt=""/><div><h1>Gmail</h1><p>Authorize an account. Each Flow configures its own operations and Triggers.</p></div></header>
    <ConnectionStatus provider="Gmail" state={connection.state === "authorization_pending" || connection.state === "broker_unavailable" ? "error" : connection.state} detail={connection.accountEmail ?? connection.detail} onConnect={onConnect} onReconnect={onReconnect}/>
    {connection.state === "connected" && <p><strong>Primary sender:</strong> {connection.accountEmail}</p>}
  </main>;
}

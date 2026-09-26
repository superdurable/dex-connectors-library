import { ConnectionStatus, type ConnectorConnectionView } from "@superdurable/dex-connectors-react";

export interface SlackSetupViewProps {
  connection: ConnectorConnectionView;
  onConnect(): void;
  onReconnect(): void;
}

export function SlackSetupView({connection, onConnect, onReconnect}: SlackSetupViewProps) {
  return <main className="card">
    <header><img src="./icon.svg" alt=""/><div><h1>Slack</h1><p>Authorize a workspace connection. Each Flow configures its operations and Triggers separately.</p></div></header>
    <ConnectionStatus provider="Slack" state={connection.state === "authorization_pending" || connection.state === "broker_unavailable" ? "error" : connection.state} detail={connection.detail} onConnect={onConnect} onReconnect={onReconnect}/>
    {connection.state === "connected" && <p className="note">Connection ready. Dex displays each Flow's reusable Slack configuration units below this connection.</p>}
  </main>;
}

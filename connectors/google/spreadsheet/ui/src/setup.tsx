import { ConnectionStatus, type ConnectorConnectionView } from "@superdurable/dex-connectors-react";

export interface SpreadsheetSetupViewProps { connection: ConnectorConnectionView; onConnect(): void; onReconnect(): void; }

export function SpreadsheetSetupView({connection, onConnect, onReconnect}: SpreadsheetSetupViewProps) {
  return <main className="card">
    <header><img src="./icon.svg" alt=""/><div><h1>Google Sheets</h1><p>Authorize an account. Each Flow chooses its own spreadsheet, tab, and operation values.</p></div></header>
    <ConnectionStatus provider="Google Sheets" state={connection.state === "authorization_pending" || connection.state === "broker_unavailable" ? "error" : connection.state} detail={connection.accountEmail ?? connection.detail} onConnect={onConnect} onReconnect={onReconnect}/>
  </main>;
}

import { ConnectionStatus, type ConnectorConnectionView, type ConnectorStudioCommand } from "@superdurable/dex-connectors-react";

export interface SpreadsheetSelection {
  spreadsheetId?: string;
  spreadsheetName?: string;
  tab?: string;
}

export interface SpreadsheetSetupViewProps {
  connection: ConnectorConnectionView;
  selection: SpreadsheetSelection;
  tabs: string[];
  capabilities: string[];
  busy?: boolean;
  onCommand(command: ConnectorStudioCommand["command"], input?: Record<string, unknown>): void;
}

export function SpreadsheetSetupView({ connection, selection, tabs, capabilities, busy, onCommand }: SpreadsheetSetupViewProps) {
  const connected = connection.state === "connected";
  const supportsPicker = capabilities.includes("google.picker.spreadsheets");
  const supportsTabList = capabilities.includes("google.sheets.tabs-list");
  return <main className="card">
    <header><img src="./icon.svg" alt=""/><div><h1>Google Sheets</h1><p>Choose the customer spreadsheet and tab.</p></div></header>
    <ConnectionStatus
      provider="Google Sheets"
      state={connection.state === "authorization_pending" || connection.state === "broker_unavailable" ? "error" : connection.state}
      detail={connection.accountEmail ?? connection.detail}
      onConnect={() => onCommand("oauth.connect")}
      onReconnect={() => onCommand("oauth.reconnect")}
    />
    {connected && <section className="controls">
      <button disabled={busy || !supportsPicker} onClick={() => onCommand("google.picker.open-spreadsheet")}>Choose spreadsheet</button>
      {!supportsPicker && <p role="note">Spreadsheet Picker is unavailable in this Dex version. Enter the spreadsheet ID manually.</p>}
      {selection.spreadsheetName && <p><strong>Spreadsheet:</strong> {selection.spreadsheetName}</p>}
      <label>Spreadsheet ID<input value={selection.spreadsheetId ?? ""} onChange={(event) => onCommand("configuration.save", { spreadsheetId: event.target.value })}/></label>
      <label>Tab<select disabled={!supportsTabList} value={selection.tab ?? ""} onChange={(event) => onCommand("configuration.save", { tab: event.target.value })}>
        <option value="">Select a tab</option>{tabs.map((tab) => <option key={tab}>{tab}</option>)}
      </select></label>
      {!supportsTabList && <p role="note">Tab discovery is unavailable in this Dex version.</p>}
      <button className="secondary" onClick={() => onCommand("oauth.revoke")}>Revoke connection</button>
    </section>}
  </main>;
}

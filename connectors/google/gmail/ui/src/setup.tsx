import { ConnectionStatus, type ConnectorConnectionView, type ConnectorStudioCommand } from "@superdurable/dex-connectors-react";

export interface GmailSetupViewProps {
  connection: ConnectorConnectionView;
  selections: GmailTriggerSelections;
  busy?: boolean;
  onCommand(command: ConnectorStudioCommand["command"]): void;
  onSelectionsChange(selections: GmailTriggerSelections): void;
  onSave(): void;
}

export interface GmailTriggerSelections {
  searchQuery?: string;
  messageContains?: string;
  senderEmails?: string[];
  replyMessageContains?: string;
  replySenderEmails?: string[];
}

export function GmailSetupView({ connection, selections, busy, onCommand, onSelectionsChange, onSave }: GmailSetupViewProps) {
  return <main className="card">
    <header><img src="./icon.svg" alt=""/><div><h1>Gmail</h1><p>Receive, read, and reply to Gmail threads.</p></div></header>
    <ConnectionStatus
      provider="Gmail"
      state={connection.state === "authorization_pending" || connection.state === "broker_unavailable" ? "error" : connection.state}
      detail={connection.accountEmail ?? connection.detail}
      onConnect={() => onCommand("oauth.connect")}
      onReconnect={() => onCommand("oauth.reconnect")}
    />
    {connection.state === "connected" && <section className="controls">
      <p><strong>Primary sender:</strong> {connection.accountEmail}</p>
      <label>Gmail search query<input value={selections.searchQuery ?? ""} placeholder="Optional, for example label:inbox" onChange={(event) => onSelectionsChange({...selections, searchQuery: event.target.value})}/></label>
      <label>Start message contains<input value={selections.messageContains ?? ""} placeholder="Optional subject or snippet text" onChange={(event) => onSelectionsChange({...selections, messageContains: event.target.value})}/></label>
      <label>Allowed root senders<input value={(selections.senderEmails ?? []).join(", ")} placeholder="Optional comma-separated email addresses" onChange={(event) => onSelectionsChange({...selections, senderEmails: emailList(event.target.value)})}/></label>
      <label>Reply message contains<input value={selections.replyMessageContains ?? ""} placeholder="Optional subject or snippet text" onChange={(event) => onSelectionsChange({...selections, replyMessageContains: event.target.value})}/></label>
      <label>Allowed reply senders<input value={(selections.replySenderEmails ?? []).join(", ")} placeholder="Optional comma-separated email addresses" onChange={(event) => onSelectionsChange({...selections, replySenderEmails: emailList(event.target.value)})}/></label>
      <p className="note">Aliases are not available in alpha. Local Triggers poll the newest inbox messages.</p>
      <button disabled={busy} onClick={onSave}>Save trigger settings</button>
    </section>}
  </main>;
}

function emailList(value: string): string[] {
  return value.split(",").map((item) => item.trim()).filter(Boolean);
}

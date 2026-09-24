import { ConnectionStatus, type ConnectorConnectionView } from "@superdurable/dex-connectors-react";

export interface SlackChannel {
  id: string;
  name: string;
  isPrivate?: boolean;
}

export interface SlackUser {
  id: string;
  displayName: string;
  imageUrl?: string;
}

export interface MessageMatcherConfiguration {
  messageContains?: string;
  posterUserIds?: string[];
}

export interface SlackTriggerSelections {
  channelId?: string;
  threadTriggerMatcher?: MessageMatcherConfiguration;
  threadReplyMatcher?: MessageMatcherConfiguration;
}

export interface SlackSetupViewProps {
  connection: ConnectorConnectionView;
  channels: SlackChannel[];
  users: SlackUser[];
  selections: SlackTriggerSelections;
  busy?: boolean;
  onConnect(): void;
  onReconnect(): void;
  onLoadChannels(): void;
  onLoadUsers(): void;
  onSelectionsChange(selections: SlackTriggerSelections): void;
  onSave(): void;
}

export function SlackSetupView(props: SlackSetupViewProps) {
  const connected = props.connection.state === "connected";
  const rootPosters = props.selections.threadTriggerMatcher?.posterUserIds ?? [];
  const approvers = props.selections.threadReplyMatcher?.posterUserIds ?? [];
  const selectUsers = (values: string[], reply: boolean) => {
    const next = {...props.selections};
    if (reply) next.threadReplyMatcher = {...next.threadReplyMatcher, posterUserIds: values};
    else next.threadTriggerMatcher = {...next.threadTriggerMatcher, posterUserIds: values};
    props.onSelectionsChange(next);
  };
  return <main className="card">
    <header><img src="./icon.svg" alt=""/><div><h1>Slack</h1><p>Start and approve Dex Flows from channel threads.</p></div></header>
    <ConnectionStatus provider="Slack" state={props.connection.state === "authorization_pending" || props.connection.state === "broker_unavailable" ? "error" : props.connection.state} detail={props.connection.detail} onConnect={props.onConnect} onReconnect={props.onReconnect}/>
    {connected && <section className="controls">
      <p className="note">Socket Mode also requires an app-level token. Enter it in the host-owned secret field outside this panel.</p>
      <div className="actions"><button disabled={props.busy} onClick={props.onLoadChannels}>Load channels</button><button disabled={props.busy} onClick={props.onLoadUsers}>Load members</button></div>
      <label>Approval channel
        <select value={props.selections.channelId ?? ""} onChange={(event) => props.onSelectionsChange({...props.selections, channelId: event.target.value})}>
          <option value="">Select a channel</option>{props.channels.map((channel) => <option key={channel.id} value={channel.id}>#{channel.name}{channel.isPrivate ? " (private)" : ""} · {channel.id}</option>)}
        </select>
      </label>
      <label>Channel ID fallback<input value={props.selections.channelId ?? ""} placeholder="C0123456789" onChange={(event) => props.onSelectionsChange({...props.selections, channelId: event.target.value})}/></label>
      <label>Start when the top-level message contains
        <input value={props.selections.threadTriggerMatcher?.messageContains ?? ""} placeholder="Optional; empty matches every message" onChange={(event) => props.onSelectionsChange({...props.selections, threadTriggerMatcher: {...props.selections.threadTriggerMatcher, messageContains: event.target.value}})}/>
      </label>
      <UserPicker label="Members allowed to start approval" users={props.users} selected={rootPosters} emptyLabel="Any human member" onChange={(values) => selectUsers(values, false)}/>
      <label>Approval reply contains
        <input value={props.selections.threadReplyMatcher?.messageContains ?? "approve"} onChange={(event) => props.onSelectionsChange({...props.selections, threadReplyMatcher: {...props.selections.threadReplyMatcher, messageContains: event.target.value}})}/>
      </label>
      <UserPicker label="Members allowed to approve" users={props.users} selected={approvers} emptyLabel="Choose at least one approver" onChange={(values) => selectUsers(values, true)}/>
      <details><summary>Find IDs manually</summary><p>Open channel details and choose “Copy channel ID”. Open a member profile, choose the menu, and copy the member ID.</p></details>
      <button disabled={props.busy || !props.selections.channelId || approvers.length === 0} onClick={props.onSave}>Save trigger settings</button>
    </section>}
  </main>;
}

function UserPicker(props: {label: string; users: SlackUser[]; selected: string[]; emptyLabel: string; onChange(values: string[]): void}) {
  return <fieldset><legend>{props.label}</legend>{props.users.length === 0 && <p className="note">{props.emptyLabel}. Member IDs may also be entered below.</p>}
    {props.users.map((user) => <label className="user" key={user.id}><input type="checkbox" checked={props.selected.includes(user.id)} onChange={(event) => props.onChange(event.target.checked ? [...props.selected, user.id] : props.selected.filter((id) => id !== user.id))}/>{user.imageUrl && <img src={user.imageUrl} alt=""/>}<span>{user.displayName}<small>{user.id}</small></span><button type="button" className="copy" onClick={() => void navigator.clipboard?.writeText(user.id)}>Copy ID</button></label>)}
    <label>Member IDs fallback<input value={props.selected.join(", ")} placeholder="U0123456789" onChange={(event) => props.onChange(event.target.value.split(",").map((value) => value.trim()).filter(Boolean))}/></label>
  </fieldset>;
}

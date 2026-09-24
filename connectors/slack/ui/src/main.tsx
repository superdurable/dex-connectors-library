import { StrictMode, useEffect, useMemo, useState } from "react";
import { createRoot } from "react-dom/client";
import { connectorStudioHostAPIVersion, isConnectorStudioMessage, type ConnectorStudioCommand, type ConnectorStudioCommandResult, type ConnectorStudioHostReady } from "@superdurable/dex-connectors-react";
import { SlackSetupView, type SlackChannel, type SlackTriggerSelections, type SlackUser } from "./setup.js";

const connectorId = "slack";
const startBindingName = "slack-thread-approval-start";
const approvalBindingName = "slack-thread-approval-reply";

function ConnectorApp() {
  const [ready, setReady] = useState<ConnectorStudioHostReady>();
  const [channels, setChannels] = useState<SlackChannel[]>([]);
  const [users, setUsers] = useState<SlackUser[]>([]);
  const [selections, setSelections] = useState<SlackTriggerSelections>({threadReplyMatcher: {messageContains: "approve", posterUserIds: []}});
  const [busy, setBusy] = useState(false);
  const pending = useMemo(() => new Map<string, ConnectorStudioCommand["command"]>(), []);

  useEffect(() => {
    const receive = (event: MessageEvent<unknown>) => {
      if (event.source !== window.parent || !isConnectorStudioMessage(event.data)) return;
      if (event.data.type === "connector.host.ready" && event.data.connectorId === connectorId) {
        setReady(event.data);
        const start = event.data.triggerBindings?.channelThreadCreated?.[startBindingName] ?? {};
        const approval = event.data.triggerBindings?.threadReplyCreated?.[approvalBindingName] ?? {};
        setSelections({channelId: stringValue(start.channelId) || stringValue(approval.channelId), threadTriggerMatcher: matcherValue(start.threadTriggerMatcher), threadReplyMatcher: matcherValue(approval.threadReplyMatcher, "approve")});
        return;
      }
      if (event.data.type !== "connector.command.result" || !ready || event.data.sessionNonce !== ready.sessionNonce) return;
      const result = event.data as ConnectorStudioCommandResult;
      const command = pending.get(result.requestId); pending.delete(result.requestId); setBusy(pending.size > 0);
      if (!result.ok) return;
      if (command === "slack.channels.list") setChannels(arrayValue<SlackChannel>(result.value?.channels));
      if (command === "slack.users.list") setUsers(arrayValue<SlackUser>(result.value?.users));
    };
    window.addEventListener("message", receive);
    return () => window.removeEventListener("message", receive);
  }, [pending, ready]);

  const send = (command: ConnectorStudioCommand["command"], input?: Record<string, unknown>) => {
    if (!ready || !ready.capabilities.includes(requiredCapability(command))) return;
    const requestId = crypto.randomUUID(); pending.set(requestId, command); setBusy(true);
    const message: ConnectorStudioCommand = {type: "connector.command", protocolVersion: connectorStudioHostAPIVersion, sessionNonce: ready.sessionNonce, connectorId, requestId, command, input};
    window.parent.postMessage(message, "*");
  };
  const save = () => {
    send("trigger.configuration.save", {triggerName: "channelThreadCreated", bindingName: startBindingName, configuration: {channelId: selections.channelId, threadTriggerMatcher: selections.threadTriggerMatcher ?? {}}});
    send("trigger.configuration.save", {triggerName: "threadReplyCreated", bindingName: approvalBindingName, configuration: {channelId: selections.channelId, threadReplyMatcher: selections.threadReplyMatcher ?? {messageContains: "approve"}}});
  };
  if (!ready) return <p role="status">Waiting for Studio…</p>;
  return <SlackSetupView connection={ready.connection} channels={channels} users={users} selections={selections} busy={busy} onConnect={() => send("oauth.connect")} onReconnect={() => send("oauth.reconnect")} onLoadChannels={() => send("slack.channels.list")} onLoadUsers={() => send("slack.users.list")} onSelectionsChange={setSelections} onSave={save}/>;
}

function requiredCapability(command: ConnectorStudioCommand["command"]): string {
  if (command.startsWith("oauth.")) return "oauth.connection.manage";
  if (command === "slack.channels.list") return "slack.channels-list";
  if (command === "slack.users.list") return "slack.users-list";
  if (command === "trigger.configuration.save") return "trigger.configuration.write";
  return "configuration.write";
}

function stringValue(value: unknown): string | undefined { return typeof value === "string" ? value : undefined; }
function matcherValue(value: unknown, defaultMessage?: string) {
  if (typeof value !== "object" || value === null || Array.isArray(value)) return {messageContains: defaultMessage, posterUserIds: []};
  const matcher = value as Record<string, unknown>;
  return {messageContains: stringValue(matcher.messageContains) ?? defaultMessage, posterUserIds: Array.isArray(matcher.posterUserIds) ? matcher.posterUserIds.filter((item): item is string => typeof item === "string") : []};
}
function arrayValue<T>(value: unknown): T[] { return Array.isArray(value) ? value as T[] : []; }

const style = document.createElement("style");
style.textContent = `:root{font-family:Inter,system-ui;color:#241524;background:#fbf8fb}body{margin:0}.card{padding:24px}header{display:flex;gap:12px;align-items:center}header img{width:42px;height:42px}h1{font-size:20px;margin:0}p{margin:5px 0;color:#635363}.controls{display:grid;gap:14px;margin-top:18px}.actions{display:flex;gap:8px}label{display:grid;gap:5px;font-weight:600}input,select,button{font:inherit;padding:9px;border:1px solid #c9b8c9;border-radius:8px}button{background:#4a154b;color:white;border:0;cursor:pointer}button:disabled{opacity:.5}.note,small{font-weight:400;color:#756575}fieldset{display:grid;gap:8px;border:1px solid #d8cbd8;border-radius:10px}.user{display:grid;grid-template-columns:auto 28px 1fr auto;align-items:center}.user img{width:24px;height:24px;border-radius:6px}.user small{display:block}.copy{background:white;color:#4a154b;border:1px solid #c9b8c9;padding:5px}`;
document.head.append(style);
createRoot(document.getElementById("root")!).render(<StrictMode><ConnectorApp/></StrictMode>);

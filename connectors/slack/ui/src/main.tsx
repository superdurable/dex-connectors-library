import { StrictMode, useEffect, useMemo, useState } from "react";
import { createRoot } from "react-dom/client";
import { connectorStudioHostAPIVersion, isConnectorStudioMessage, observeConnectorStudioFrameAutoHeight, type ConnectorStudioCommand, type ConnectorStudioCommandResult, type ConnectorStudioHostReady } from "@superdurable/dex-connectors-react";
import { SlackSetupView } from "./setup.js";
import { SlackConfigurationUnit, type SlackChannel, type SlackUser } from "./units.js";

const connectorId = "slack";

function ConnectorApp() {
  const [ready, setReady] = useState<ConnectorStudioHostReady>();
  const [channels, setChannels] = useState<SlackChannel[]>([]);
  const [users, setUsers] = useState<SlackUser[]>([]);
  const [busy, setBusy] = useState(false);
  const pending = useMemo(() => new Map<string, ConnectorStudioCommand["command"]>(), []);
  useEffect(() => ready ? observeConnectorStudioFrameAutoHeight(ready) : undefined, [ready]);

  useEffect(() => {
    const receive = (event: MessageEvent<unknown>) => {
      if (event.source !== window.parent || !isConnectorStudioMessage(event.data) || event.data.connectorId !== connectorId) return;
      if (event.data.type === "connector.host.ready") { setReady(event.data); return; }
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
    window.parent.postMessage({type: "connector.command", protocolVersion: connectorStudioHostAPIVersion, sessionNonce: ready.sessionNonce, connectorId, requestId, command, input} satisfies ConnectorStudioCommand, "*");
  };
  if (!ready) return <p role="status">Waiting for Studio…</p>;
  if (ready.target.kind === "connection") return <SlackSetupView connection={ready.connection} onConnect={() => send("oauth.connect")} onReconnect={() => send("oauth.reconnect")}/>;
  return <SlackConfigurationUnit target={ready.target} channels={channels} users={users} busy={busy} onLoadChannels={() => send("slack.channels.list")} onLoadUsers={() => send("slack.users.list")} onSave={(value) => send("use.configuration.save", {value})}/>;
}

function requiredCapability(command: ConnectorStudioCommand["command"]): string {
  if (command.startsWith("oauth.")) return "oauth.connection.manage";
  if (command === "slack.channels.list") return "slack.channels-list";
  if (command === "slack.users.list") return "slack.users-list";
  return "use.configuration.write";
}

function arrayValue<T>(value: unknown): T[] { return Array.isArray(value) ? value as T[] : []; }

const style = document.createElement("style");
style.textContent = `:root{font-family:Inter,system-ui;color:#241524;background:#fbf8fb}body{margin:0}.card,.unit{padding:24px}header{display:flex;gap:12px;align-items:center}header img{width:42px;height:42px}h1,h2{font-size:20px;margin:0}p{margin:5px 0;color:#635363}.unit{display:grid;gap:12px}label{display:grid;gap:5px;font-weight:600}input,select,button{font:inherit;padding:9px;border:1px solid #c9b8c9;border-radius:8px}button{background:#4a154b;color:white;border:0;cursor:pointer}button:disabled{opacity:.5}.note,small{font-weight:400;color:#756575}.user{grid-template-columns:auto 28px 1fr;align-items:center}.user img{width:24px;height:24px;border-radius:6px}.user small{display:block}`;
document.head.append(style);
createRoot(document.getElementById("root")!).render(<StrictMode><ConnectorApp/></StrictMode>);

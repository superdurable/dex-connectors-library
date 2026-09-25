import { StrictMode, useEffect, useMemo, useState } from "react";
import { createRoot } from "react-dom/client";
import {
  connectorStudioHostAPIVersion,
  isConnectorStudioMessage,
  type ConnectorStudioCommand,
  type ConnectorStudioHostReady,
} from "@superdurable/dex-connectors-react";
import { GmailSetupView, type GmailTriggerSelections } from "./setup.js";

const connectorId = "gmail";
const startBindingName = "gmail-thread-reply-start";
const replyBindingName = "gmail-thread-reply-received";

function ConnectorApp() {
  const [ready, setReady] = useState<ConnectorStudioHostReady>();
  const [selections, setSelections] = useState<GmailTriggerSelections>({});
  const [busy, setBusy] = useState(false);
  const pending = useMemo(() => new Set<string>(), []);
  useEffect(() => {
    const receive = (event: MessageEvent<unknown>) => {
      if (event.source !== window.parent || !isConnectorStudioMessage(event.data) || event.data.connectorId !== connectorId) return;
      if (event.data.type === "connector.host.ready") {
        setReady(event.data);
        const start = event.data.triggerBindings?.messageReceived?.[startBindingName] ?? {};
        const reply = event.data.triggerBindings?.replyReceived?.[replyBindingName] ?? {};
        const startMatcher = recordValue(start.messageMatcher);
        const replyMatcher = recordValue(reply.replyMatcher);
        setSelections({
          searchQuery: stringValue(start.searchQuery) || stringValue(reply.searchQuery),
          messageContains: stringValue(startMatcher.messageContains), senderEmails: stringArray(startMatcher.senderEmails),
          replyMessageContains: stringValue(replyMatcher.messageContains), replySenderEmails: stringArray(replyMatcher.senderEmails),
        });
        return;
      }
      if (event.data.type === "connector.command.result" && ready && event.data.sessionNonce === ready.sessionNonce) {
        pending.delete(event.data.requestId); setBusy(pending.size > 0);
      }
    };
    window.addEventListener("message", receive);
    return () => window.removeEventListener("message", receive);
  }, [pending, ready]);
  const send = (command: ConnectorStudioCommand["command"], input?: Record<string, unknown>) => {
    const capability = command === "trigger.configuration.save" ? "trigger.configuration.write" : "oauth.connection.manage";
    if (!ready || !ready.capabilities.includes(capability)) return;
    const requestId = crypto.randomUUID(); pending.add(requestId); setBusy(true);
    const message: ConnectorStudioCommand = { type: "connector.command", protocolVersion: connectorStudioHostAPIVersion, sessionNonce: ready.sessionNonce, connectorId, requestId, command, input };
    window.parent.postMessage(message, "*");
  };
  const save = () => {
    send("trigger.configuration.save", {triggerName: "messageReceived", bindingName: startBindingName, configuration: {
      searchQuery: selections.searchQuery, messageMatcher: {messageContains: selections.messageContains, senderEmails: selections.senderEmails ?? []},
    }});
    send("trigger.configuration.save", {triggerName: "replyReceived", bindingName: replyBindingName, configuration: {
      searchQuery: selections.searchQuery, replyMatcher: {messageContains: selections.replyMessageContains, senderEmails: selections.replySenderEmails ?? []},
    }});
  };
  if (!ready) return <p role="status">Waiting for Studio…</p>;
  return <GmailSetupView connection={ready.connection} selections={selections} busy={busy} onCommand={send} onSelectionsChange={setSelections} onSave={save}/>;
}

function recordValue(value: unknown): Record<string, unknown> { return typeof value === "object" && value !== null && !Array.isArray(value) ? value as Record<string, unknown> : {}; }
function stringValue(value: unknown): string | undefined { return typeof value === "string" ? value : undefined; }
function stringArray(value: unknown): string[] { return Array.isArray(value) ? value.filter((item): item is string => typeof item === "string") : []; }

const style = document.createElement("style");
style.textContent = `:root{font-family:Inter,system-ui;color:#281b1b;background:#fff8f7}body{margin:0}.card{padding:24px}header{display:flex;gap:12px;align-items:center}header img{width:42px;height:42px}h1{font-size:20px;margin:0}p{margin:5px 0;color:#675252}.controls{display:grid;gap:12px;margin-top:18px}.controls label{display:grid;gap:5px;font-weight:600}.controls input{font:inherit;padding:9px;border:1px solid #d8b8b8;border-radius:8px}.note{font-size:13px}button{font:inherit;padding:9px;border-radius:8px;cursor:pointer}.secondary{background:white;color:#8a2525;border:1px solid #d8b8b8}`;
document.head.append(style);
createRoot(document.getElementById("root")!).render(<StrictMode><ConnectorApp/></StrictMode>);

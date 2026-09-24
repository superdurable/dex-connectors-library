import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { GmailSetupView } from "../src/setup.js";

describe("Gmail setup", () => {
  it("shows only the verified primary sender", () => {
    const markup = renderToStaticMarkup(<GmailSetupView connection={{state: "connected", accountEmail: "owner@example.com", grantedScopes: ["openid", "email"]}} onCommand={() => undefined}/>);
    expect(markup).toContain("owner@example.com");
    expect(markup).toContain("Aliases are not available");
    expect(markup).not.toContain("access_token");
    expect(markup).not.toContain("Revoke connection");
  });

  it.each([
    ["revoked", "Connection revoked"],
    ["insufficient_scope", "Additional permission required"],
  ] as const)("shows a reconnect action for %s", (state, label) => {
    const markup = renderToStaticMarkup(<GmailSetupView connection={{state, grantedScopes: []}} onCommand={() => undefined}/>);
    expect(markup).toContain(label);
    expect(markup).toContain("Reconnect");
    expect(markup).not.toContain("Primary sender");
  });
});

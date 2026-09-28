// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT
// @vitest-environment happy-dom

import { afterEach, describe, expect, it } from "vitest";

import {
  applyConnectorStudioTheme,
  connectorStudioHostAPIVersion,
  connectorStudioStyles,
  connectorStudioStylesheetMaxLength,
  connectorStudioThemeTokenNames,
  isConnectorStudioStylesheet,
  isConnectorStudioThemeTokenValue,
  selectConnectorStudioThemeTokens,
  type ConnectorStudioHostReady,
} from "../src/index.js";

const hostReady = (fields: Partial<ConnectorStudioHostReady> = {}): ConnectorStudioHostReady => ({
  type: "connector.host.ready",
  protocolVersion: connectorStudioHostAPIVersion,
  sessionNonce: "nonce",
  connectorId: "gmail",
  capabilities: [],
  connection: {state: "connected", grantedScopes: []},
  target: {kind: "connection"},
  ...fields,
});

const darkHostTokens = {
  "--studio-surface-card": "#10151a",
  "--studio-ink-max": "#f2f4f7",
  "--studio-cta": "#70eea9",
  "--studio-accent-wash": "rgba(137, 190, 255, 0.1)",
  "--studio-focus-ring": "0 0 0 3px rgba(137, 190, 255, 0.3)",
  "--studio-radius": "8px",
  "--studio-duration": "120ms",
  "--studio-font-sans": "Inter, 'Segoe UI', -apple-system, sans-serif",
};

const fontFamilies = (count: number) => Array.from({length: count}, (_, index) => `Font${index}`).join(", ");

// Keep identical to the list in Dex web/app/v2/connections/connectorStudioTheme.test.ts.
const sharedThemeTokenGrammarCases = {
  color: {
    accepted: [
      "#abc", "#abcd", "#a1b2c3", "#A1B2C3D4", "rgb(0, 134, 80)", "rgb(0,134,80)", "rgb(255, 255, 255)",
      "rgba(52, 101, 159, 0.08)", "rgba(137, 190, 255, .1)", "rgba(0, 0, 0, 0)", "rgba(0, 0, 0, 1)",
      "rgba(0, 0, 0, 1.0)", "rgba(0,0,0,1.00)",
    ],
    rejected: [
      "#abcde", "#ggg", "red", "transparent", "currentColor", "rgb(0 0 0)", "rgb(0 0 0 / 50%)", "rgb(256, 0, 0)",
      "rgb(999,0,0)", "rgb(-1, 0, 0)", "rgb(10%,0%,0%)", "rgb(01, 0, 0)", "rgba(0,0,0,50%)", "rgba(0,0,0)",
      "rgb(0,0,0,0.5)", "rgba(0, 0, 0, 1.5)", "rgba(0, 0, 0, 0.12345)", "RGB(0, 0, 0)", "hsl(0, 0%, 0%)",
      "color-mix(in srgb, red, blue)", "#fff /* */", "#fff;", "",
    ],
  },
  focusRing: {
    accepted: ["0 0 0 3px rgba(137, 190, 255, 0.3)", "0 0 0 3px rgba(137, 190, 255, .3)", "0 0 0 2px #34659f", "inset 0 0 0 1px rgb(0, 0, 0)", "0 -1px 2px #000"],
    rejected: [
      "none", "0 0 0 3px", "3px #000", "0 0 0 3em #000", "0 0 0 3px #000, 0 0 0 1px #fff", "0 0 0 3px #000 inset",
      "0 0 0 0 0 #000", "0 0 0 3px transparent", "0 0 0 3.1234px #000",
    ],
  },
  fontStack: {
    accepted: [
      "Inter", "ui-monospace, monospace", "'SF Mono', Menlo", "\"Helvetica Neue\", Arial, sans-serif", "Helvetica Neue, sans-serif",
      "-apple-system, BlinkMacSystemFont", "'Font 2', serif", "Nonesuch, serif", "'inherit', serif", fontFamilies(16),
    ],
    rejected: [
      "Font 2", "'My_Font'", "'My.Font'", "'unterminated, Arial", "Inter, url(x)", "Inter\\", "Inter;", "Inter, }", "Arial, 'x' y",
      "Inter,,Arial", "inherit", "initial", "unset", "revert", "revert-layer", "default", "none", "INHERIT", "inherit, Arial",
      "Arial, initial", fontFamilies(17), "a".repeat(257),
    ],
  },
  length: {
    accepted: ["0", "4px", "6.5px", "4.125px", "999px"],
    rejected: ["6", "-4px", "4rem", "4.1234px", "1000px", "4px 4px", "4px;", "calc(1px + 2px)"],
  },
  duration: {
    accepted: ["160ms", "0ms", "9999ms", ".16s", "0.16s", "1s", "12.5s"],
    rejected: ["160", "0", "-160ms", "10000ms", "160 ms", "1.5ms", "100s", ".1234s", "calc(1s)"],
  },
};

const tokenNameForKind: Record<keyof typeof sharedThemeTokenGrammarCases, string> = {
  color: "--studio-cta",
  focusRing: "--studio-focus-ring",
  fontStack: "--studio-font-sans",
  length: "--studio-radius",
  duration: "--studio-duration",
};

const root = () => document.documentElement;
const inlineToken = (name: string) => root().style.getPropertyValue(name).trim();
const installedStylesheet = () => document.getElementById("dex-connector-studio-theme")?.textContent;
const hostStylesheet = `${connectorStudioStyles}\n.studio-button { border-radius: 999px; padding: 10px 20px; }\n`;

afterEach(() => {
  document.getElementById("dex-connector-studio-theme")?.remove();
  root().removeAttribute("style");
  delete root().dataset.theme;
});

describe("applyConnectorStudioTheme", () => {
  it("installs the embedded Dex Web styles once and defaults to light", () => {
    expect(applyConnectorStudioTheme(hostReady())).toBe("light");
    expect(applyConnectorStudioTheme()).toBe("light");
    expect(document.querySelectorAll("#dex-connector-studio-theme")).toHaveLength(1);
    expect(installedStylesheet()).toBe(connectorStudioStyles);
    expect(root().dataset.theme).toBe("light");
    expect(root().getAttribute("style") ?? "").toBe("");
  });

  it("replaces the embedded stylesheet with the host's in the same <style> element", () => {
    applyConnectorStudioTheme(hostReady());
    const style = document.getElementById("dex-connector-studio-theme");
    applyConnectorStudioTheme(hostReady({stylesheet: hostStylesheet}));
    expect(document.querySelectorAll("style")).toHaveLength(1);
    expect(document.getElementById("dex-connector-studio-theme")).toBe(style);
    expect(installedStylesheet()).toBe(hostStylesheet);
  });

  it("keeps the embedded stylesheet when the host's is not acceptable", () => {
    const rejectedStylesheets: unknown[] = [
      "", "  \n", 7, {css: connectorStudioStyles}, [connectorStudioStyles], null,
      `${connectorStudioStyles}</style><script>alert(1)</script>`,
      ".studio-surface { gap: 20px; }",
      connectorStudioStyles + " ".repeat(connectorStudioStylesheetMaxLength),
    ];
    for (const stylesheet of rejectedStylesheets) {
      applyConnectorStudioTheme(hostReady({stylesheet: stylesheet as string}));
      expect(installedStylesheet(), String(stylesheet).slice(0, 40)).toBe(connectorStudioStyles);
    }
  });

  it("restores the embedded stylesheet when a later ready message omits the host's", () => {
    applyConnectorStudioTheme(hostReady({stylesheet: hostStylesheet}));
    applyConnectorStudioTheme(hostReady({theme: "dark"}));
    expect(installedStylesheet()).toBe(connectorStudioStyles);
    expect(root().dataset.theme).toBe("dark");
  });

  it("applies host tokens over the host stylesheet's token defaults", () => {
    const stylesheet = `${connectorStudioStyles}\n:root { --studio-cta: #111111; }\n`;
    const computedCta = () => getComputedStyle(root()).getPropertyValue("--studio-cta").trim();
    applyConnectorStudioTheme(hostReady({stylesheet}));
    expect(computedCta()).toBe("#111111");
    applyConnectorStudioTheme(hostReady({stylesheet, themeTokens: {"--studio-cta": "#222222"}}));
    expect(inlineToken("--studio-cta")).toBe("#222222");
    expect(computedCta()).toBe("#222222");
  });

  it("honors a dark host theme and ignores unknown themes", () => {
    expect(applyConnectorStudioTheme(hostReady({theme: "dark"}))).toBe("dark");
    expect(root().dataset.theme).toBe("dark");
    expect(applyConnectorStudioTheme(hostReady({theme: "sepia" as "dark"}))).toBe("light");
    expect(root().dataset.theme).toBe("light");
  });

  it("applies valid dark host tokens as inline custom properties", () => {
    expect(applyConnectorStudioTheme(hostReady({theme: "dark", themeTokens: darkHostTokens}))).toBe("dark");
    for (const [name, value] of Object.entries(darkHostTokens)) expect(inlineToken(name)).toBe(value);
    expect(inlineToken("--studio-surface-page")).toBe("");
  });

  it("rejects injection attempts without dropping the valid tokens beside them", () => {
    applyConnectorStudioTheme(hostReady({themeTokens: {
      "--studio-cta": "red; background: url(https://attacker.example/leak)",
      "--studio-surface-card": "#fff} body { display: none",
      "--studio-surface-page": "url(https://attacker.example/pixel.png)",
      "--studio-ink-max": "var(--studio-cta)",
      "--studio-focus-ring": "0 0 0 3px rgba(0, 0, 0, 0.3) !important",
      "--studio-font-sans": "Inter; color: red",
      "--studio-radius": "expression(alert(1))",
      "--studio-accent": "#34659f",
    }}));
    expect(inlineToken("--studio-accent")).toBe("#34659f");
    for (const name of ["--studio-cta", "--studio-surface-card", "--studio-surface-page", "--studio-ink-max", "--studio-focus-ring", "--studio-font-sans", "--studio-radius"]) {
      expect(inlineToken(name)).toBe("");
    }
    expect(root().getAttribute("style")).not.toMatch(/url|attacker|display|var\(|important|expression|;\s*color/);
  });

  it("ignores names outside the allowlist and malformed token maps", () => {
    applyConnectorStudioTheme(hostReady({themeTokens: {
      "--studio-unknown": "#ffffff",
      "--surface-card": "#ffffff",
      "background": "#ffffff",
      "--studio-cta": "#008650",
    }}));
    expect(inlineToken("--studio-cta")).toBe("#008650");
    expect(inlineToken("--studio-unknown")).toBe("");
    expect(inlineToken("--surface-card")).toBe("");
    expect(root().style.getPropertyValue("background")).toBe("");
    for (const themeTokens of [null, "--studio-cta: red", ["#ffffff"], 7]) {
      expect(selectConnectorStudioThemeTokens(themeTokens).size).toBe(0);
    }
  });

  it("replaces host tokens when a later ready message arrives", () => {
    applyConnectorStudioTheme(hostReady({theme: "dark", themeTokens: darkHostTokens}));
    applyConnectorStudioTheme(hostReady({theme: "dark", themeTokens: {"--studio-cta": "#87fab9"}}));
    expect(inlineToken("--studio-cta")).toBe("#87fab9");
    expect(inlineToken("--studio-surface-card")).toBe("");

    expect(applyConnectorStudioTheme(hostReady())).toBe("light");
    expect(root().dataset.theme).toBe("light");
    for (const name of connectorStudioThemeTokenNames) expect(inlineToken(name)).toBe("");
  });
});

const paddedTo = (stylesheet: string, length: number) => stylesheet + " ".repeat(length - stylesheet.length);
const withoutRuleFor = (className: string) => connectorStudioStyles.replaceAll(new RegExp(String.raw`\.${className}(?![\w-])[^{]*\{[^}]*\}`, "g"), "");

describe("isConnectorStudioStylesheet", () => {
  it("accepts a stylesheet up to the length bound that selects every contract class", () => {
    expect(isConnectorStudioStylesheet(connectorStudioStyles)).toBe(true);
    expect(isConnectorStudioStylesheet(hostStylesheet)).toBe(true);
    expect(isConnectorStudioStylesheet(paddedTo(connectorStudioStyles, connectorStudioStylesheetMaxLength))).toBe(true);
    expect(isConnectorStudioStylesheet(`${connectorStudioStyles}.studio-option-label::before { content: '<'; }`)).toBe(true);
  });

  it("rejects a non-string or oversized stylesheet and any closing tag sequence", () => {
    const rejected: unknown[] = [
      undefined, null, 7, true, {}, [], "", " \t\n",
      paddedTo(connectorStudioStyles, connectorStudioStylesheetMaxLength + 1),
      ...["</style>", "</STYLE >", "</", "/* </script> */"].map((closingTag) => `${connectorStudioStyles}${closingTag}`),
    ];
    for (const stylesheet of rejected) expect(isConnectorStudioStylesheet(stylesheet), String(stylesheet).slice(-40)).toBe(false);
  });

  it("rejects a stylesheet that leaves a contract class without a selector", () => {
    for (const className of ["studio-badge", "studio-option", "studio-notice-attention", "studio-surface"]) {
      const stylesheet = withoutRuleFor(className);
      expect(stylesheet).not.toMatch(new RegExp(String.raw`\.${className}(?![\w-])`));
      expect(isConnectorStudioStylesheet(stylesheet), className).toBe(false);
    }
    expect(isConnectorStudioStylesheet(".studio-badges, .studio-options { gap: 0; }")).toBe(false);
  });
});

describe("Studio theme token allowlist", () => {
  it("matches the tokens the embedded stylesheet declares, whose defaults pass validation", () => {
    const declarations = [...connectorStudioStyles.matchAll(/(--studio-[a-z-]+): ([^;]+);/g)];
    expect(new Set(declarations.map(([, name]) => name))).toEqual(new Set(connectorStudioThemeTokenNames));
    for (const [, name, value] of declarations) expect(isConnectorStudioThemeTokenValue(name, value.trim()), `${name}: ${value}`).toBe(true);
  });

  it("applies the grammar shared with Dex Web to every token kind", () => {
    for (const [kind, {accepted, rejected}] of Object.entries(sharedThemeTokenGrammarCases)) {
      const name = tokenNameForKind[kind as keyof typeof sharedThemeTokenGrammarCases];
      for (const value of accepted) expect(isConnectorStudioThemeTokenValue(name, value), `${name}: ${value}`).toBe(true);
      for (const value of rejected) expect(isConnectorStudioThemeTokenValue(name, value), `${name}: ${value}`).toBe(false);
    }
  });

  it("rejects values that are not strings and names outside the allowlist", () => {
    const rejected: [string, unknown][] = [
      ["--studio-cta", "rgb(0, 134, 80)) url(x"], ["--studio-cta", "\\23 fff"], ["--studio-cta", "#fff\n}"],
      ["--studio-cta", "</style><script>alert(1)</script>"], ["--studio-cta", 7], ["--studio-font-sans", "'Inter'), url(x"],
      ["__proto__", "#fff"], ["constructor", "#fff"], ["--studio-unknown", "#fff"],
    ];
    for (const [name, value] of rejected) expect(isConnectorStudioThemeTokenValue(name, value), `${name}: ${String(value)}`).toBe(false);
  });
});

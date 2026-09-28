// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { ConnectorStudioHostReady } from "./host-api.js";

/** ConnectorStudioTheme is the Dex Web colour theme a bundle renders with. */
export type ConnectorStudioTheme = "light" | "dark";

const styleElementId = "dex-connector-studio-theme";

type ThemeTokenValueKind = "color" | "focusRing" | "fontStack" | "length" | "duration";

const themeTokenValueKinds = {
  "--studio-surface-page": "color",
  "--studio-surface-card": "color",
  "--studio-surface-raised": "color",
  "--studio-surface-hover": "color",
  "--studio-ink-max": "color",
  "--studio-ink-strong": "color",
  "--studio-ink-mid": "color",
  "--studio-ink-soft": "color",
  "--studio-line-soft": "color",
  "--studio-line-mid": "color",
  "--studio-line-strong": "color",
  "--studio-cta": "color",
  "--studio-cta-hover": "color",
  "--studio-cta-on": "color",
  "--studio-accent": "color",
  "--studio-accent-wash": "color",
  "--studio-focus-ring": "focusRing",
  "--studio-success-ink": "color",
  "--studio-success-fill": "color",
  "--studio-danger-ink": "color",
  "--studio-danger-fill": "color",
  "--studio-attention-ink": "color",
  "--studio-attention-fill": "color",
  "--studio-font-sans": "fontStack",
  "--studio-font-mono": "fontStack",
  "--studio-radius-sm": "length",
  "--studio-radius": "length",
  "--studio-radius-lg": "length",
  "--studio-duration": "duration",
} as const satisfies Record<string, ThemeTokenValueKind>;

/** ConnectorStudioThemeTokenName is one --studio-* custom property a host may override. */
export type ConnectorStudioThemeTokenName = keyof typeof themeTokenValueKinds;

/**
 * connectorStudioThemeTokenNames is the allowlist of --studio-* custom
 * properties that a host's ready.themeTokens may override. It is exactly the
 * set of tokens connectorStudioStyles declares.
 */
export const connectorStudioThemeTokenNames: readonly ConnectorStudioThemeTokenName[] =
  Object.freeze(Object.keys(themeTokenValueKinds) as ConnectorStudioThemeTokenName[]);

// Dex Web web/app/v2/connections/connectorStudioTheme.ts applies this same grammar; change both together.
const maxThemeTokenValueLength = 256;
const hexColorPattern = "#(?:[0-9a-fA-F]{3,4}|[0-9a-fA-F]{6}|[0-9a-fA-F]{8})";
const colorChannelPattern = "(?:25[0-5]|2[0-4][0-9]|1[0-9]{2}|[1-9]?[0-9])";
const colorAlphaPattern = String.raw`(?:0(?:\.[0-9]{1,4})?|1(?:\.0{1,4})?|\.[0-9]{1,4})`;
const rgbColorPattern = String.raw`rgb\( *${colorChannelPattern} *, *${colorChannelPattern} *, *${colorChannelPattern} *\)`;
const rgbaColorPattern = String.raw`rgba\( *${colorChannelPattern} *, *${colorChannelPattern} *, *${colorChannelPattern} *, *${colorAlphaPattern} *\)`;
const colorPattern = `(?:${hexColorPattern}|${rgbColorPattern}|${rgbaColorPattern})`;
const pixelLengthPattern = String.raw`(?:0|[0-9]{1,3}(?:\.[0-9]{1,3})?px)`;
const durationPattern = String.raw`(?:[0-9]{1,4}ms|(?:[0-9]{1,2}(?:\.[0-9]{1,3})?|\.[0-9]{1,3})s)`;
// CSS requires these words to be quoted in a family name.
const reservedFontFamilyWordPattern = "(?:inherit|initial|unset|revert|revert-layer|default|none)(?![A-Za-z0-9-])";
const unquotedFontFamilyWordPattern = `(?!${reservedFontFamilyWordPattern})-?[A-Za-z][A-Za-z0-9-]*`;
const fontFamilyPattern = `(?:${unquotedFontFamilyWordPattern}(?: ${unquotedFontFamilyWordPattern})*|'[A-Za-z0-9 -]+'|"[A-Za-z0-9 -]+")`;
const themeTokenValuePatterns: Record<ThemeTokenValueKind, RegExp> = {
  color: new RegExp(`^${colorPattern}$`),
  focusRing: new RegExp(`^(?:inset +)?-?${pixelLengthPattern}(?: +-?${pixelLengthPattern}){1,3} +${colorPattern}$`),
  fontStack: new RegExp(`^${fontFamilyPattern}(?: *, *${fontFamilyPattern}){0,15}$`, "i"),
  length: new RegExp(`^${pixelLengthPattern}$`),
  duration: new RegExp(`^${durationPattern}$`),
};

/**
 * isConnectorStudioThemeTokenValue reports whether value is an acceptable
 * host override for token name. Dex Web validates the tokens it sends with the
 * same grammar:
 *
 *   - Colours are #hex with 3, 4, 6, or 8 digits, rgb(R, G, B), or
 *     rgba(R, G, B, A) in comma syntax. Channels are integers from 0 to 255
 *     without leading zeros; alpha is 0 to 1 with at most four decimals.
 *   - Radii are 0 or a non-negative px length with at most three digits on
 *     each side of the decimal point.
 *   - --studio-focus-ring is one box-shadow: an optional inset, two to four px
 *     lengths that may be negative, and one colour.
 *   - --studio-duration is up to four digits of ms, or seconds with at most
 *     two integer digits and three decimals, such as 160ms or .16s.
 *   - Fonts are 1 to 16 comma-separated families. A family is a quoted name of
 *     letters, digits, spaces, and hyphens, or unquoted words that each start
 *     with a letter, optionally after one hyphen. The CSS-wide keywords,
 *     default, and none are accepted only when quoted.
 *
 * Everything else is rejected, including url(), var(), other functions,
 * semicolons, braces, !important, and values longer than 256 characters.
 * Names outside connectorStudioThemeTokenNames are always rejected.
 */
export function isConnectorStudioThemeTokenValue(name: string, value: unknown): boolean {
  if (!Object.hasOwn(themeTokenValueKinds, name) || typeof value !== "string" || value.length > maxThemeTokenValueLength) return false;
  return themeTokenValuePatterns[themeTokenValueKinds[name as ConnectorStudioThemeTokenName]].test(value);
}

/**
 * selectConnectorStudioThemeTokens returns the entries of a host's
 * themeTokens that pass isConnectorStudioThemeTokenValue. Unknown names and
 * rejected values are dropped individually; anything but a plain object
 * yields an empty map.
 */
export function selectConnectorStudioThemeTokens(themeTokens: unknown): Map<ConnectorStudioThemeTokenName, string> {
  const accepted = new Map<ConnectorStudioThemeTokenName, string>();
  if (typeof themeTokens !== "object" || themeTokens === null || Array.isArray(themeTokens)) return accepted;
  for (const [name, value] of Object.entries(themeTokens)) {
    if (isConnectorStudioThemeTokenValue(name, value)) accepted.set(name as ConnectorStudioThemeTokenName, value as string);
  }
  return accepted;
}

// Token values mirror the Dex Web v2 ramps (web/app/v2/css/tokens.css), so a
// sandboxed bundle, which cannot load host CSS files, matches the Connections page.
const lightTokens = `
  color-scheme: light;
  --studio-surface-page: #f2f8f4; --studio-surface-card: #ffffff; --studio-surface-raised: #f7fcf9; --studio-surface-hover: #e9efeb;
  --studio-ink-max: #181a1c; --studio-ink-strong: #434547; --studio-ink-mid: #6e7073; --studio-ink-soft: #949699;
  --studio-line-soft: #e4e6e9; --studio-line-mid: #d3d5d8; --studio-line-strong: #abacaf;
  --studio-cta: #008650; --studio-cta-hover: #007546; --studio-cta-on: #ffffff;
  --studio-accent: #34659f; --studio-accent-wash: rgba(52, 101, 159, 0.08); --studio-focus-ring: 0 0 0 3px rgba(52, 101, 159, 0.32);
  --studio-success-ink: #1c754a; --studio-success-fill: #ddf6e6;
  --studio-danger-ink: #9a4548; --studio-danger-fill: #ffe8e8;
  --studio-attention-ink: #845b00; --studio-attention-fill: #fbecd6;`;

const darkTokens = `
  color-scheme: dark;
  --studio-surface-page: #0e120f; --studio-surface-card: #1a1c1e; --studio-surface-raised: #212623; --studio-surface-hover: #272c29;
  --studio-ink-max: #f2f4f7; --studio-ink-strong: #c1c3c5; --studio-ink-mid: #8f9193; --studio-ink-soft: #6e7072;
  --studio-line-soft: #262729; --studio-line-mid: #303133; --studio-line-strong: #4c4e50;
  --studio-cta: #70eea9; --studio-cta-hover: #87fab9; --studio-cta-on: #070a08;
  --studio-accent: #89beff; --studio-accent-wash: rgba(137, 190, 255, 0.1); --studio-focus-ring: 0 0 0 3px rgba(137, 190, 255, 0.3);
  --studio-success-ink: #7cd09f; --studio-success-fill: #072c19;
  --studio-danger-ink: #fb9c9c; --studio-danger-fill: #3c1719;
  --studio-attention-ink: #e1b267; --studio-attention-fill: #332000;`;

/**
 * connectorStudioClassNames is the Studio class contract: every studio-* class
 * that a shared component or a bundle may render. connectorStudioStyles styles
 * each one, and so does the stylesheet Dex Web sends as ready.stylesheet.
 * Bundles render these classes and bring no styles of their own, so the host
 * owns how released bundles look. Adding a class is additive. A host
 * stylesheet that drops a class sends every bundle built with it back to its
 * compiled stylesheet (see isConnectorStudioStylesheet).
 *
 * Some rules also style elements inside a class, such as the img, h1, h2, and
 * p of .studio-header or the input of .studio-option; sdk/react/README.md lists
 * the markup each class expects.
 */
export const connectorStudioClassNames = Object.freeze([
  "studio-surface",
  "studio-header",
  "studio-muted",
  "studio-field",
  "studio-actions",
  "studio-button",
  "studio-button-primary",
  "studio-notice",
  "studio-notice-info",
  "studio-notice-success",
  "studio-notice-error",
  "studio-notice-attention",
  "studio-options",
  "studio-option",
  "studio-option-label",
  "studio-option-id",
  "studio-option-detail",
  "studio-badges",
  "studio-badge",
  "studio-checkbox",
] as const);

/** ConnectorStudioClassName is one class in the connectorStudioClassNames contract. */
export type ConnectorStudioClassName = (typeof connectorStudioClassNames)[number];

// Dex Web web/app/v2/connections/connectorStudio.css is the canonical copy; keep these rules identical to it.
/**
 * connectorStudioStyles is the stylesheet compiled into every Studio bundle.
 * applyConnectorStudioTheme installs it when the host sends no acceptable
 * ready.stylesheet, which is what hosts older than the stylesheet field get.
 * It styles every class in connectorStudioClassNames, and plain form controls
 * inside a .studio-surface, with Dex Web's typography, radii, and CTA green.
 */
export const connectorStudioStyles = `
:root { ${lightTokens}
  --studio-font-sans: -apple-system, BlinkMacSystemFont, 'Segoe UI', system-ui, 'Helvetica Neue', Arial, sans-serif;
  --studio-font-mono: ui-monospace, 'SF Mono', SFMono-Regular, Menlo, Consolas, 'Liberation Mono', monospace;
  --studio-radius-sm: 4px; --studio-radius: 6px; --studio-radius-lg: 10px; --studio-duration: 160ms;
}
:root[data-theme='dark'] { ${darkTokens} }
html, body { margin: 0; background: transparent; }
body { font: 13px/1.45 var(--studio-font-sans); color: var(--studio-ink-max); }
.studio-surface { display: grid; gap: 14px; padding: 18px; border: 1px solid var(--studio-line-mid); border-radius: var(--studio-radius-lg); background: var(--studio-surface-card); }
.studio-header { display: flex; gap: 12px; align-items: center; }
.studio-header img { width: 32px; height: 32px; border-radius: var(--studio-radius); }
.studio-header h1, .studio-header h2 { margin: 0; font-size: 15px; font-weight: 650; color: var(--studio-ink-max); }
.studio-header p, .studio-muted { margin: 2px 0 0; color: var(--studio-ink-mid); }
.studio-field, .studio-field > label { display: grid; gap: 5px; font-size: 12px; font-weight: 650; color: var(--studio-ink-strong); }
.studio-field small { font-weight: 400; color: var(--studio-ink-mid); }
.studio-surface input[type='text'], .studio-surface input[type='search'], .studio-surface input:not([type]), .studio-surface select {
  font: inherit; font-weight: 400; padding: 8px 10px; border: 1px solid var(--studio-line-mid); border-radius: 7px;
  background: var(--studio-surface-page); color: var(--studio-ink-max);
}
.studio-surface input:focus-visible, .studio-surface select:focus-visible, .studio-button:focus-visible, .studio-option:focus-within {
  outline: none; box-shadow: var(--studio-focus-ring); border-color: var(--studio-accent);
}
.studio-actions { display: flex; flex-wrap: wrap; gap: 8px; align-items: center; }
.studio-button { font: inherit; font-size: 12px; font-weight: 600; padding: 8px 12px; border-radius: var(--studio-radius); cursor: pointer;
  border: 1px solid var(--studio-line-mid); background: transparent; color: var(--studio-ink-strong); transition: background var(--studio-duration); }
.studio-button:hover:not(:disabled) { background: var(--studio-surface-hover); }
.studio-button-primary { border-color: var(--studio-cta); background: var(--studio-cta); color: var(--studio-cta-on); }
.studio-button-primary:hover:not(:disabled) { border-color: var(--studio-cta-hover); background: var(--studio-cta-hover); }
.studio-button:disabled { cursor: default; border-color: var(--studio-line-mid); background: var(--studio-surface-raised); color: var(--studio-ink-mid); }
.studio-notice { margin: 0; padding: 8px 10px; border-radius: var(--studio-radius); font-size: 12px; }
.studio-notice-info { background: var(--studio-accent-wash); color: var(--studio-accent); }
.studio-notice-success { background: var(--studio-success-fill); color: var(--studio-success-ink); }
.studio-notice-error { background: var(--studio-danger-fill); color: var(--studio-danger-ink); }
.studio-notice-attention { background: var(--studio-attention-fill); color: var(--studio-attention-ink); }
.studio-options { display: grid; gap: 2px; max-height: 320px; overflow: auto; margin: 0; padding: 4px; border: 1px solid var(--studio-line-mid); border-radius: 7px; background: var(--studio-surface-page); }
.studio-option { display: grid; grid-template-columns: auto 1fr; gap: 2px 8px; align-items: start; padding: 7px 8px; border-radius: var(--studio-radius-sm); cursor: pointer; font-weight: 400; }
.studio-option:hover { background: var(--studio-surface-hover); }
.studio-option input { margin: 2px 0 0; accent-color: var(--studio-cta); }
.studio-option-label { color: var(--studio-ink-max); font-weight: 600; }
.studio-option-label img { width: 20px; height: 20px; margin-right: 6px; border-radius: var(--studio-radius-sm); vertical-align: -5px; }
.studio-option-id { grid-column: 2; font-family: var(--studio-font-mono); font-size: 11px; color: var(--studio-ink-mid); overflow-wrap: anywhere; }
.studio-option-detail { grid-column: 2; font-size: 12px; color: var(--studio-ink-mid); }
.studio-badges { grid-column: 2; display: flex; flex-wrap: wrap; gap: 4px; }
.studio-badge { padding: 1px 6px; border-radius: 999px; background: var(--studio-accent-wash); color: var(--studio-accent); font-size: 11px; }
.studio-checkbox { display: flex; gap: 6px; align-items: center; font-size: 12px; font-weight: 400; color: var(--studio-ink-strong); }
.studio-checkbox input { accent-color: var(--studio-cta); }
`;

/**
 * connectorStudioStylesheetMaxLength is the longest ready.stylesheet a bundle
 * applies, measured as a JavaScript string length in UTF-16 code units. It is
 * 64 KiB for an ASCII stylesheet.
 */
export const connectorStudioStylesheetMaxLength = 65_536;

const contractClassSelectorPatterns = connectorStudioClassNames.map((className) => new RegExp(String.raw`\.${className}(?![\w-])`));

/**
 * isConnectorStudioStylesheet reports whether a host's ready.stylesheet may
 * replace connectorStudioStyles. It must be a string of at most
 * connectorStudioStylesheetMaxLength that contains no "</", so it can never
 * close the <style> element that holds it, and that has a class selector for
 * every class in this bundle's connectorStudioClassNames. Because of the last
 * rule, a bundle that renders a class an older Dex Web does not know keeps the
 * stylesheet it was built with instead of losing that class's rules. The check
 * looks for ".studio-*" selector text; it does not parse the rules.
 *
 * It returns a plain boolean, not a type guard, because a rejected value may
 * still be a string. Narrow with typeof before using the value.
 *
 * The bundle checks nothing else, because every check it makes is frozen
 * into released bundles. The stricter authoring rules in sdk/react/README.md,
 * which connectorStudioStyles and Dex Web's canonical stylesheet follow, live
 * with the host, where a Dex Web release can change them.
 */
export function isConnectorStudioStylesheet(stylesheet: unknown): boolean {
  return typeof stylesheet === "string"
    && stylesheet.length <= connectorStudioStylesheetMaxLength
    && !stylesheet.includes("</")
    && contractClassSelectorPatterns.every((pattern) => pattern.test(stylesheet));
}

/**
 * applyConnectorStudioTheme installs the Studio stylesheet and selects the
 * theme. The stylesheet is the host's ready.stylesheet when it passes
 * isConnectorStudioStylesheet and the compiled connectorStudioStyles
 * otherwise, so Dex Web owns every rule of a released bundle while older hosts
 * keep the look the bundle was built with. Both live in one managed <style>
 * element whose text each call replaces. A host that reports `theme` in its
 * ready message wins; otherwise the bundle uses Dex Web's default light theme,
 * because the sandboxed frame cannot observe the host's choice.
 *
 * It then sets the host's accepted ready.themeTokens (see
 * selectConnectorStudioThemeTokens) as inline custom properties on
 * document.documentElement, which take precedence over the token defaults in
 * either stylesheet, so a Dex Web restyle reaches a bundle without a
 * connector release. Tokens the host omits or that fail validation keep the
 * stylesheet's defaults, which is what older hosts get. Call it again for
 * every ready message: each call replaces the previous host stylesheet and
 * tokens. A message without an acceptable stylesheet restores
 * connectorStudioStyles, and tokens it no longer carries are removed.
 */
export function applyConnectorStudioTheme(ready?: ConnectorStudioHostReady): ConnectorStudioTheme {
  const theme: ConnectorStudioTheme = ready?.theme === "dark" ? "dark" : "light";
  const hostStylesheet = ready?.stylesheet;
  const stylesheet = typeof hostStylesheet === "string" && isConnectorStudioStylesheet(hostStylesheet) ? hostStylesheet : connectorStudioStyles;
  let style = document.getElementById(styleElementId);
  if (!style) {
    style = document.createElement("style");
    style.id = styleElementId;
    document.head.append(style);
  }
  // Rewriting identical text would still make the browser reparse the sheet.
  if (style.textContent !== stylesheet) style.textContent = stylesheet;
  const root = document.documentElement;
  root.dataset.theme = theme;
  const hostTokens = selectConnectorStudioThemeTokens(ready?.themeTokens);
  for (const name of connectorStudioThemeTokenNames) {
    const value = hostTokens.get(name);
    if (value === undefined) root.style.removeProperty(name);
    else root.style.setProperty(name, value);
  }
  return theme;
}

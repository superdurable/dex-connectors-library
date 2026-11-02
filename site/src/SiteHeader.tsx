// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { DexMark } from "./DexMark";

// Mirrors the superdurable.io header; resync markup and styles.css `.sd-*` rules together.

type SuperDurableNavigationLink = {
  label: string;
  href: string;
  isCurrentSite?: boolean;
};

const superDurableHomeUrl = "https://superdurable.io/";
const superDurableGitHubUrl = "https://github.com/superdurable";
const dexLogInUrl = "https://dex.superdurable.io/login";
const dexSignUpUrl = "https://dex.superdurable.io/register";

const superDurableNavigationLinks: SuperDurableNavigationLink[] = [
  { label: "Dex Engine", href: "https://superdurable.io/dex-engine/" },
  { label: "Dex Cloud", href: "https://superdurable.io/dex-cloud/" },
  { label: "Dex Connectors", href: "https://connectors.superdurable.io/", isCurrentSite: true },
  { label: "Dex Docs", href: "https://docs.superdurable.io" },
  { label: "Blog", href: "https://superdurable.io/blog/" },
  { label: "About", href: "https://superdurable.io/about/" },
];

export function SiteHeader() {
  return (
    <header className="sd-header">
      <div className="sd-header-inner">
        <a className="sd-wordmark" aria-label="Super Durable home" href={superDurableHomeUrl}>
          <DexMark size={28} />
          <span>Super Durable</span>
        </a>
        <nav className="sd-nav" aria-label="Primary navigation">
          <SuperDurableNavigationAnchors />
        </nav>
        <div className="sd-header-actions">
          <a className="sd-quiet-link" href={superDurableGitHubUrl}>GitHub</a>
          <a className="sd-quiet-link" href={dexLogInUrl}>Log in</a>
          <a className="sd-button sd-button-primary sd-button-small" href={dexSignUpUrl}>Sign up</a>
        </div>
        <details className="sd-mobile-menu">
          <summary aria-label="Open navigation">Menu</summary>
          <nav aria-label="Mobile navigation">
            <SuperDurableNavigationAnchors />
            <a href={superDurableGitHubUrl}>GitHub</a>
            <a href={dexLogInUrl}>Log in</a>
            <a className="sd-button sd-button-primary" href={dexSignUpUrl}>Sign up</a>
          </nav>
        </details>
      </div>
    </header>
  );
}

function SuperDurableNavigationAnchors() {
  return (
    <>
      {superDurableNavigationLinks.map((navigationLink) => (
        <a
          key={navigationLink.label}
          href={navigationLink.href}
          aria-current={navigationLink.isCurrentSite ? "page" : undefined}
        >
          {navigationLink.label}
        </a>
      ))}
    </>
  );
}

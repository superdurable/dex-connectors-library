// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { useEffect, useRef } from "react";

// Follows the shared superdurable.io header spec; resync markup and styles.css `.sd-*` rules together.

type SuperDurableNavigationLink = {
  label: string;
  href: string;
  isCurrentSite?: boolean;
};

const superDurableHomeUrl = "https://superdurable.io/";
const dexRepositoryGitHubUrl = "https://github.com/superdurable/dex";
const dexLogInUrl = "https://dex.superdurable.io/login";

const superDurableNavigationLinks: SuperDurableNavigationLink[] = [
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
          <svg className="brand-symbol" aria-hidden="true" focusable="false" viewBox="0 0 64 64" width="28" height="28">
            <path d="M 13.5 12 V 52" fill="none" stroke="var(--brand-track)" strokeLinecap="round" strokeWidth="12" />
            <path
              d="M 30.5 12 L 50.5 32 L 30.5 52"
              fill="none"
              stroke="var(--brand-head)"
              strokeLinecap="round"
              strokeLinejoin="round"
              strokeWidth="12"
            />
          </svg>
          <span>Super Durable</span>
        </a>
        <nav className="sd-nav" aria-label="Primary navigation">
          <SuperDurableNavigationAnchors />
        </nav>
        <div className="sd-header-actions">
          <a className="sd-quiet-link" href={dexRepositoryGitHubUrl}>GitHub</a>
          <a className="sd-quiet-link" href={dexLogInUrl}>Log in</a>
        </div>
        <MobileMenu />
      </div>
    </header>
  );
}

// The native <details> stays uncontrolled so it toggles without script; listeners only add closing paths.
function MobileMenu() {
  const menuRef = useRef<HTMLDetailsElement>(null);
  const summaryRef = useRef<HTMLElement>(null);

  useEffect(() => {
    const menu = menuRef.current;
    const summary = summaryRef.current;
    if (!menu || !summary) {
      return;
    }
    const closeOnEscape = (event: KeyboardEvent) => {
      if (event.key === "Escape" && menu.open) {
        menu.open = false;
        summary.focus();
      }
    };
    const closeOnOutsidePointerDown = (event: PointerEvent) => {
      if (menu.open && event.target instanceof Node && !menu.contains(event.target)) {
        menu.open = false;
      }
    };
    document.addEventListener("keydown", closeOnEscape);
    document.addEventListener("pointerdown", closeOnOutsidePointerDown);
    return () => {
      document.removeEventListener("keydown", closeOnEscape);
      document.removeEventListener("pointerdown", closeOnOutsidePointerDown);
    };
  }, []);

  return (
    <details className="sd-mobile-menu" ref={menuRef}>
      <summary ref={summaryRef}>Menu</summary>
      <nav aria-label="Mobile navigation">
        <SuperDurableNavigationAnchors />
        <a href={dexRepositoryGitHubUrl}>GitHub</a>
        <a href={dexLogInUrl}>Log in</a>
      </nav>
    </details>
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

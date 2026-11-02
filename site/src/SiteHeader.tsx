// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { useEffect, useRef, useState } from "react";

// Follows the shared superdurable.io header spec; resync markup and styles.css `.sd-*` rules together.

type SuperDurableNavigationLink = {
  label: string;
  href: string;
  isCurrentSite?: boolean;
};

const superDurableHomeUrl = "https://superdurable.io/";
const dexRepositoryGitHubUrl = "https://github.com/superdurable/dex";
const dexLogInUrl = "https://dex.superdurable.io/login";
const dexRepositoryApiUrl = "https://api.github.com/repos/superdurable/dex";
const starCountCacheKey = "superdurable-dex-star-count";
// Unauthenticated GitHub API allows 60 requests per hour per IP.
const starCountCacheMilliseconds = 60 * 60 * 1000;

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
          <GitHubRepositoryLink />
          <a className="sd-quiet-link" href={dexLogInUrl}>Log in</a>
        </div>
        <MobileMenu />
      </div>
    </header>
  );
}

function GitHubRepositoryLink() {
  const starCount = useDexRepositoryStarCount();
  const accessibleLabel = starCount === null ? "Dex on GitHub" : `Dex on GitHub, ${starCount} ${starCount === 1 ? "star" : "stars"}`;
  return (
    <a className="sd-github-link" href={dexRepositoryGitHubUrl} aria-label={accessibleLabel}>
      <span className="sd-github-link-name">
        <svg aria-hidden="true" focusable="false" viewBox="0 0 16 16" width="16" height="16">
          <path
            fill="currentColor"
            d="M8 0C3.58 0 0 3.58 0 8c0 3.54 2.29 6.53 5.47 7.59.4.07.55-.17.55-.38 0-.19-.01-.82-.01-1.49-2.01.37-2.53-.49-2.69-.94-.09-.23-.48-.94-.82-1.13-.28-.15-.68-.52-.01-.53.63-.01 1.08.58 1.23.82.72 1.21 1.87.87 2.33.66.07-.52.28-.87.51-1.07-1.78-.2-3.64-.89-3.64-3.95 0-.87.31-1.59.82-2.15-.08-.2-.36-1.02.08-2.12 0 0 .67-.21 2.2.82.64-.18 1.32-.27 2-.27.68 0 1.36.09 2 .27 1.53-1.04 2.2-.82 2.2-.82.44 1.1.16 1.92.08 2.12.51.56.82 1.27.82 2.15 0 3.07-1.87 3.75-3.65 3.95.29.25.54.73.54 1.48 0 1.07-.01 1.93-.01 2.2 0 .21.15.46.55.38A8.013 8.013 0 0 0 16 8c0-4.42-3.58-8-8-8Z"
          />
        </svg>
        GitHub
      </span>
      {starCount === null ? null : (
        <span className="sd-github-link-stars">
          <svg aria-hidden="true" focusable="false" viewBox="0 0 16 16" width="14" height="14">
            <path
              fill="currentColor"
              d="M8 .25a.75.75 0 0 1 .673.418l1.882 3.815 4.21.612a.75.75 0 0 1 .416 1.279l-3.046 2.97.719 4.192a.751.751 0 0 1-1.088.791L8 12.347l-3.766 1.98a.75.75 0 0 1-1.088-.79l.72-4.194L.818 6.374a.75.75 0 0 1 .416-1.28l4.21-.611L7.327.668A.75.75 0 0 1 8 .25Z"
            />
          </svg>
          {formatStarCount(starCount)}
        </span>
      )}
    </a>
  );
}

// The header renders without a count when GitHub is unreachable or rate limited.
function useDexRepositoryStarCount(): number | null {
  const [starCount, setStarCount] = useState<number | null>(readCachedStarCount);

  useEffect(() => {
    if (starCount !== null) {
      return;
    }
    const controller = new AbortController();
    fetch(dexRepositoryApiUrl, { signal: controller.signal, headers: { Accept: "application/vnd.github+json" } })
      .then((response) => (response.ok ? response.json() : null))
      .then((repository: unknown) => {
        const fetchedStarCount = (repository as { stargazers_count?: unknown } | null)?.stargazers_count;
        if (typeof fetchedStarCount === "number") {
          setStarCount(fetchedStarCount);
          writeCachedStarCount(fetchedStarCount);
        }
      })
      .catch(() => {
        // Best effort: the link still works without a count.
      });
    return () => controller.abort();
  }, [starCount]);

  return starCount;
}

function readCachedStarCount(): number | null {
  try {
    const cached = JSON.parse(window.localStorage.getItem(starCountCacheKey) ?? "null") as
      | { starCount?: unknown; fetchedAt?: unknown }
      | null;
    if (
      cached &&
      typeof cached.starCount === "number" &&
      typeof cached.fetchedAt === "number" &&
      Date.now() >= cached.fetchedAt &&
      Date.now() - cached.fetchedAt < starCountCacheMilliseconds
    ) {
      return cached.starCount;
    }
  } catch {
    // Storage may be blocked or hold malformed data; fetch instead.
  }
  return null;
}

function writeCachedStarCount(starCount: number) {
  try {
    window.localStorage.setItem(starCountCacheKey, JSON.stringify({ starCount, fetchedAt: Date.now() }));
  } catch {
    // Storage may be blocked; the next visit fetches again.
  }
}

function formatStarCount(starCount: number): string {
  return starCount >= 1000 ? `${(starCount / 1000).toFixed(1).replace(/\.0$/, "")}k` : String(starCount);
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

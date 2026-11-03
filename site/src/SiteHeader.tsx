// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { useEffect, useRef, useState } from "react";

import { GitHubMarkIcon } from "./GitHubMarkIcon";

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
const failedFetchCacheMilliseconds = 10 * 60 * 1000;
const starCountFetchTimeoutMilliseconds = 5000;

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
  const accessibleLabel =
    starCount === null ? undefined : `GitHub, ${formatStarCount(starCount)} ${starCount === 1 ? "star" : "stars"}`;
  return (
    <a className="sd-github-link" href={dexRepositoryGitHubUrl} aria-label={accessibleLabel}>
      <span className="sd-github-link-name">
        <GitHubMarkIcon />
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
  const [starCount, setStarCount] = useState<number | null>(null);

  useEffect(() => {
    const cachedStarCount = readCachedStarCount();
    if (cachedStarCount !== undefined) {
      setStarCount(cachedStarCount);
      return;
    }
    const controller = new AbortController();
    const fetchTimeout = window.setTimeout(() => controller.abort(), starCountFetchTimeoutMilliseconds);
    fetch(dexRepositoryApiUrl, { signal: controller.signal, headers: { Accept: "application/vnd.github+json" } })
      .then((response) => (response.ok ? response.json() : null))
      .then((repository: unknown) => {
        const fetchedStarCount = (repository as { stargazers_count?: unknown } | null)?.stargazers_count;
        if (isValidStarCount(fetchedStarCount)) {
          setStarCount(fetchedStarCount);
          writeCachedStarCount(fetchedStarCount);
        } else {
          writeFailedFetchUnlessCountCached();
        }
      })
      .catch(() => {
        if (!controller.signal.aborted) {
          writeFailedFetchUnlessCountCached();
        }
      })
      .finally(() => window.clearTimeout(fetchTimeout));
    return () => {
      window.clearTimeout(fetchTimeout);
      controller.abort();
    };
  }, []);

  return starCount;
}

// Returns undefined on a cache miss and null for a cached failure.
function readCachedStarCount(): number | null | undefined {
  try {
    const cached = JSON.parse(window.localStorage.getItem(starCountCacheKey) ?? "null") as
      | { starCount?: unknown; fetchedAt?: unknown }
      | null;
    if (!cached || typeof cached.fetchedAt !== "number") {
      return undefined;
    }
    const cacheAgeMilliseconds = Date.now() - cached.fetchedAt;
    if (cached.starCount === null) {
      return cacheAgeMilliseconds >= 0 && cacheAgeMilliseconds < failedFetchCacheMilliseconds ? null : undefined;
    }
    if (isValidStarCount(cached.starCount) && cacheAgeMilliseconds >= 0 && cacheAgeMilliseconds < starCountCacheMilliseconds) {
      return cached.starCount;
    }
  } catch {
    // Storage may be blocked or hold malformed data; fetch instead.
  }
  return undefined;
}

function writeCachedStarCount(starCount: number | null) {
  try {
    window.localStorage.setItem(starCountCacheKey, JSON.stringify({ starCount, fetchedAt: Date.now() }));
  } catch {
    // Storage may be blocked; the next visit fetches again.
  }
}

// Another page on this origin may have cached a fresh count meanwhile; keep it.
function writeFailedFetchUnlessCountCached() {
  if (typeof readCachedStarCount() !== "number") {
    writeCachedStarCount(null);
  }
}

function isValidStarCount(value: unknown): value is number {
  return typeof value === "number" && Number.isSafeInteger(value) && value >= 0;
}

function formatStarCount(starCount: number): string {
  return starCount >= 1000 ? `${Math.round(starCount / 100) / 10}k` : String(starCount);
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

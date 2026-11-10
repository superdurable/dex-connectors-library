// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { ReactElement } from "react";

// Hand-drawn 24 px stroke icons; they inherit the current text color.
const iconShapes = {
  sparkles: <><path d="M12 3l1.8 4.7 4.7 1.8-4.7 1.8L12 16l-1.8-4.7-4.7-1.8 4.7-1.8z" /><path d="M18.5 15l.8 2 2 .8-2 .8-.8 2-.8-2-2-.8 2-.8z" /></>,
  message: <path d="M21 12a8 8 0 0 1-11.6 7.1L4 20l1-4.6A8 8 0 1 1 21 12z" />,
  megaphone: <><path d="M3 10v4a1 1 0 0 0 1 1h3l7 4V5L7 9H4a1 1 0 0 0-1 1z" /><path d="M18 9a4 4 0 0 1 0 6" /></>,
  share: <><circle cx="18" cy="5" r="2.5" /><circle cx="6" cy="12" r="2.5" /><circle cx="18" cy="19" r="2.5" /><path d="M8.2 10.8l7.6-4.4M8.2 13.2l7.6 4.4" /></>,
  trendingUp: <><path d="M3 17l6-6 4 4 8-8" /><path d="M15 7h6v6" /></>,
  headset: <><path d="M4 14v-2a8 8 0 0 1 16 0v2" /><path d="M4 14h3v6H5a1 1 0 0 1-1-1zM20 14h-3v6h2a1 1 0 0 0 1-1z" /></>,
  calendarCheck: <><rect x="3" y="5" width="18" height="16" rx="2" /><path d="M3 10h18M8 3v4M16 3v4M9 15.5l2 2 4-4" /></>,
  kanban: <><rect x="3" y="3" width="18" height="18" rx="2" /><path d="M8 7v8M12 7v5M16 7v10" /></>,
  file: <><path d="M14 3H7a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2V8z" /><path d="M14 3v5h5M9 13h6M9 17h4" /></>,
  code: <path d="M8 6l-6 6 6 6M16 6l6 6-6 6M14 4l-4 16" />,
  database: <><ellipse cx="12" cy="5" rx="8" ry="3" /><path d="M4 5v14c0 1.7 3.6 3 8 3s8-1.3 8-3V5M4 12c0 1.7 3.6 3 8 3s8-1.3 8-3" /></>,
  barChart: <path d="M3 3v18h18M8 17v-5M13 17V8M18 17v-8" />,
  creditCard: <><rect x="2" y="5" width="20" height="14" rx="2" /><path d="M2 10h20M6 15h4" /></>,
  shoppingBag: <><path d="M5 7h14l1 13a1 1 0 0 1-1 1H5a1 1 0 0 1-1-1z" /><path d="M9 10V7a3 3 0 0 1 6 0v3" /></>,
  users: <><circle cx="9" cy="8" r="3.5" /><path d="M2.5 20a6.5 6.5 0 0 1 13 0M16 4.5a3.5 3.5 0 0 1 0 7M18 14.5a6.5 6.5 0 0 1 3.5 5.5" /></>,
  shield: <><path d="M12 3l8 3v6c0 4.5-3.4 8.3-8 9-4.6-.7-8-4.5-8-9V6z" /><path d="M9 12l2 2 4-4" /></>,
  globe: <><circle cx="12" cy="12" r="9" /><path d="M3 12h18M12 3a14 14 0 0 1 0 18 14 14 0 0 1 0-18" /></>,
  grid: <><rect x="3" y="3" width="7.5" height="7.5" rx="1.5" /><rect x="13.5" y="3" width="7.5" height="7.5" rx="1.5" /><rect x="3" y="13.5" width="7.5" height="7.5" rx="1.5" /><rect x="13.5" y="13.5" width="7.5" height="7.5" rx="1.5" /></>,
  search: <><circle cx="11" cy="11" r="7" /><path d="M20 20l-4-4" /></>,
  close: <path d="M6 6l12 12M18 6L6 18" />,
  arrowLeft: <path d="M19 12H5M11 18l-6-6 6-6" />,
  copy: <><rect x="9" y="9" width="12" height="12" rx="2" /><path d="M5 15H4a1 1 0 0 1-1-1V4a1 1 0 0 1 1-1h10a1 1 0 0 1 1 1v1" /></>,
  check: <path d="M5 12l5 5L20 7" />,
  tag: <><path d="M3 12V4a1 1 0 0 1 1-1h8l9 9-9 9z" /><circle cx="7.5" cy="7.5" r="1.5" /></>,
  bolt: <path d="M13 2L4 14h7l-1 8 9-12h-7z" />,
  eye: <><path d="M2 12s3.5-7 10-7 10 7 10 7-3.5 7-10 7S2 12 2 12z" /><circle cx="12" cy="12" r="3" /></>,
  pencil: <><path d="M4 20h4L19 9l-4-4L4 16z" /><path d="M14 6l4 4" /></>,
  layout: <><rect x="3" y="3" width="18" height="18" rx="2" /><path d="M3 9h18M9 9v12" /></>,
  inbox: <><path d="M3 13l3-8h12l3 8v6a1 1 0 0 1-1 1H4a1 1 0 0 1-1-1z" /><path d="M3 13h5l1 2h6l1-2h5" /></>,
  terminal: <><rect x="2" y="4" width="20" height="16" rx="2" /><path d="M6 9l3 3-3 3M12 15h6" /></>,
  folder: <path d="M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z" />,
} satisfies Record<string, ReactElement>;

/** Names one icon of the directory's inline icon set. */
export type IconName = keyof typeof iconShapes;

const categoryIconNames: Record<string, IconName> = {
  ai: "sparkles",
  communication: "message",
  marketing: "megaphone",
  "social-media": "share",
  "crm-sales": "trendingUp",
  "customer-support": "headset",
  productivity: "calendarCheck",
  "project-management": "kanban",
  "files-documents": "file",
  "developer-tools": "code",
  "databases-storage": "database",
  analytics: "barChart",
  "finance-payments": "creditCard",
  ecommerce: "shoppingBag",
  "hr-recruiting": "users",
  "security-identity": "shield",
  "data-search": "globe",
};

/** Renders one decorative icon, `size` pixels square, in the current text color. */
export function Icon({ name, size = 16, className }: { name: IconName; size?: number; className?: string }): ReactElement {
  return (
    <svg className={className ? `icon ${className}` : "icon"} viewBox="0 0 24 24" width={size} height={size}
      fill="none" stroke="currentColor" strokeWidth={1.8} strokeLinecap="round" strokeLinejoin="round"
      aria-hidden="true" focusable="false">
      {iconShapes[name]}
    </svg>
  );
}

/**
 * Renders the icon for a catalog category ID. The catalog owns the taxonomy, so an ID added there before an
 * icon exists here falls back to the grid icon.
 */
export function CategoryIcon({ categoryID, size = 16 }: { categoryID: string; size?: number }): ReactElement {
  return <Icon name={categoryIconNames[categoryID] ?? "grid"} size={size} />;
}

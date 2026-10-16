// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// The Dex mark: the pipe operator |> set as a D. Geometry is on a 64 grid and matches
// the canonical set; sizes of 32px and below use the heavier small cut.
export function DexMark({ size = 30 }: { size?: number }) {
  const small = size <= 32;
  const strokeWidth = small ? 12 : 10.5;
  const top = small ? 12 : 11;
  const bottom = 64 - top;
  const chevronX = small ? 30.5 : 29.5;
  return (
    <svg aria-hidden="true" focusable="false" height={size} viewBox="0 0 64 64" width={size}>
      <path
        d={`M 13.5 ${top} V ${bottom}`}
        fill="none"
        stroke="var(--brand-track)"
        strokeLinecap="round"
        strokeWidth={strokeWidth}
      />
      <path
        d={`M ${chevronX} ${top} L 50.5 32 L ${chevronX} ${bottom}`}
        fill="none"
        stroke="var(--brand-head)"
        strokeLinecap="round"
        strokeLinejoin="round"
        strokeWidth={strokeWidth}
      />
    </svg>
  );
}

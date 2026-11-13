// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { useLayoutEffect, useRef, useState, type CSSProperties, type RefObject } from "react";

/**
 * Measures the segment marked `data-selected="true"` inside a positioned container and returns the style that
 * places one shared indicator under it. The style updates whenever `selectedKey` changes or the container
 * resizes; the first placement skips the transition so the indicator never slides in from the corner.
 */
export function useSlidingSegmentIndicator(containerRef: RefObject<HTMLElement | null>, selectedKey: string):
  CSSProperties {
  const [indicatorStyle, setIndicatorStyle] = useState<CSSProperties>({ opacity: 0 });
  const hasPlacedIndicatorRef = useRef(false);

  useLayoutEffect(() => {
    const container = containerRef.current;
    if (!container) {
      return;
    }
    const placeIndicator = () => {
      const selectedSegment = container.querySelector<HTMLElement>("[data-selected='true']");
      if (!selectedSegment) {
        setIndicatorStyle({ opacity: 0 });
        return;
      }
      setIndicatorStyle({
        width: selectedSegment.offsetWidth,
        height: selectedSegment.offsetHeight,
        transform: `translate(${selectedSegment.offsetLeft}px, ${selectedSegment.offsetTop}px)`,
        transition: hasPlacedIndicatorRef.current ? undefined : "none",
      });
      hasPlacedIndicatorRef.current = true;
    };
    placeIndicator();
    const resizeObserver = new ResizeObserver(placeIndicator);
    resizeObserver.observe(container);
    return () => resizeObserver.disconnect();
  }, [containerRef, selectedKey]);

  return indicatorStyle;
}

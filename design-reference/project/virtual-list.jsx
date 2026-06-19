// Lightweight windowing for long lists. Renders only items in/near viewport.
// Use for any list that might exceed ~200 rows.
//
// Usage:
//   <VirtualList items={rows} itemHeight={22} renderItem={(row, i) => <div>...</div>} />
//
// Items must have a consistent height (or set itemHeight to an upper-bound for
// auto-measured rows). For dynamic-height content (wrapped log lines) prefer
// fixed-height rows with overflow:hidden + ellipsis.
const { useRef: useRefVL, useState: useStateVL, useEffect: useEffectVL, useLayoutEffect: useLayoutEffectVL, useCallback: useCallbackVL } = React;

function VirtualList({
  items,
  itemHeight,
  renderItem,
  header = null,
  footer = null,
  overscan = 8,
  followBottom = false,
  className = "",
  style = {},
  emptyState = null,
  onScroll,
}) {
  const ref = useRefVL();
  const [scrollTop, setScrollTop] = useStateVL(0);
  const [viewportH, setViewportH] = useStateVL(0);
  const followRef = useRefVL(followBottom);
  followRef.current = followBottom;

  useLayoutEffectVL(() => {
    const el = ref.current;
    if (!el) return;
    setViewportH(el.clientHeight);
    const ro = new ResizeObserver(() => setViewportH(el.clientHeight));
    ro.observe(el);
    return () => ro.disconnect();
  }, []);

  // When followBottom flips ON or items grow at tail, scroll to bottom
  useEffectVL(() => {
    if (!followRef.current || !ref.current) return;
    ref.current.scrollTop = ref.current.scrollHeight;
  }, [items.length, followBottom]);

  const total = items.length;
  const totalH = total * itemHeight;
  const start = Math.max(0, Math.floor(scrollTop / itemHeight) - overscan);
  const end = Math.min(total, Math.ceil((scrollTop + viewportH) / itemHeight) + overscan);
  const offsetY = start * itemHeight;
  const slice = items.slice(start, end);

  const handleScroll = useCallbackVL((e) => {
    setScrollTop(e.target.scrollTop);
    onScroll?.(e);
  }, [onScroll]);

  if (total === 0 && emptyState) {
    return <div className={className} style={{ ...style, overflow: "auto" }}>{emptyState}</div>;
  }

  return (
    <div ref={ref} className={className} onScroll={handleScroll}
         style={{ overflowY: "auto", overflowX: "hidden", position: "relative", ...style }}>
      {header}
      <div style={{ height: totalH, position: "relative" }}>
        <div style={{ position: "absolute", top: offsetY, left: 0, right: 0 }}>
          {slice.map((it, i) => (
            <div key={start + i} style={{ height: itemHeight }}>
              {renderItem(it, start + i)}
            </div>
          ))}
        </div>
      </div>
      {footer}
    </div>
  );
}

Object.assign(window, { VirtualList });

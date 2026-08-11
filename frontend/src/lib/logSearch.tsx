import React from 'react';

// Typing must stay responsive while lines stream in, so a search box owns its
// own immediate state and only hands the term to the (expensive) filter pass
// after a short idle.
export const SEARCH_DEBOUNCE_MS = 120;

/**
 * Wraps every case-insensitive occurrence of `q` in `text` in a <mark>.
 *
 * Deliberately a plain substring scan rather than a RegExp: log lines are full
 * of regex metacharacters, and a user typing `(` should get matches, not a
 * thrown pattern.
 */
export function highlight(text: string, q: string): React.ReactNode {
  if (!q) return text;
  const ql = q.toLowerCase();
  const tl = text.toLowerCase();
  const out: React.ReactNode[] = [];
  let i = 0;
  while (i < text.length) {
    const found = tl.indexOf(ql, i);
    if (found < 0) { out.push(text.slice(i)); break; }
    if (found > i) out.push(text.slice(i, found));
    out.push(<mark key={found}>{text.slice(found, found + q.length)}</mark>);
    i = found + q.length;
  }
  return out;
}

// Go marshals a nil slice as JSON `null`, but `wails generate module` types
// every slice as a plain array. So `set.splits.length` type-checks and then
// throws at runtime on any device that happens to produce an empty list —
// which is exactly how a render crash reaches the user.
//
// This script re-types the generated bindings so a slice can be null. With
// TypeScript strict mode on, every unguarded read of one becomes a compile
// error instead of a crash. It runs before `tsc` on every build (and before
// the dev server), because Wails regenerates the bindings behind our back.
//
// Idempotent: already-patched declarations are skipped.
import {readFileSync, writeFileSync} from 'node:fs';
import {fileURLToPath} from 'node:url';
import {dirname, join} from 'node:path';

const here = dirname(fileURLToPath(import.meta.url));
const models = join(here, '..', 'wailsjs', 'go', 'models.ts');
const decls = join(here, '..', 'wailsjs', 'go', 'main', 'App.d.ts');

// Struct fields: `    splits: string[];` → `    splits: string[] | null;`
const FIELD = /^(\s+)([A-Za-z_$][\w$]*)(\??): ([\w.]+\[\])(;)$/gm;
// Bindings: `Promise<Array<adb.App>>` → `Promise<Array<adb.App> | null>`
const RETURN = /Promise<Array<([\w.]+)>>/g;

function patch(file, re, replacement) {
  let src;
  try {
    src = readFileSync(file, 'utf8');
  } catch (e) {
    if (e.code === 'ENOENT') return 0; // bindings not generated yet
    throw e;
  }
  let n = 0;
  const out = src.replace(re, (...m) => { n++; return replacement(...m); });
  if (n > 0) writeFileSync(file, out);
  return n;
}

const a = patch(models, FIELD, (_, indent, name, opt, type, semi) => `${indent}${name}${opt}: ${type} | null${semi}`);
const b = patch(decls, RETURN, (_, type) => `Promise<Array<${type}> | null>`);

if (a + b > 0) console.log(`nullable-bindings: ${a} field(s), ${b} return type(s) marked nullable`);

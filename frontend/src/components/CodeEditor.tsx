import React, {useEffect, useRef} from 'react';
import {basicSetup} from 'codemirror';
import {EditorView} from '@codemirror/view';
import {EditorState, Compartment} from '@codemirror/state';
import {HighlightStyle, syntaxHighlighting} from '@codemirror/language';
import {tags as t} from '@lezer/highlight';
import {javascript} from '@codemirror/lang-javascript';

// The editor is themed entirely through the app's CSS variables, so it tracks
// dark/light automatically (the tokens change with [data-theme]) without this
// component ever reading the theme.
const editorTheme = EditorView.theme({
  '&': {backgroundColor: 'var(--bg-inset)', color: 'var(--text)', height: '100%', fontSize: '12.5px'},
  '&.cm-focused': {outline: 'none'},
  '.cm-scroller': {fontFamily: 'var(--font-mono)', lineHeight: '1.55'},
  '.cm-content': {caretColor: 'var(--accent)'},
  '.cm-cursor, .cm-dropCursor': {borderLeftColor: 'var(--accent)'},
  '.cm-gutters': {backgroundColor: 'var(--bg-inset)', color: 'var(--text-subtle)', border: 'none'},
  '.cm-activeLine': {backgroundColor: 'var(--hover)'},
  '.cm-activeLineGutter': {backgroundColor: 'var(--hover)', color: 'var(--text-muted)'},
  '.cm-selectionBackground, &.cm-focused .cm-selectionBackground, .cm-content ::selection': {
    backgroundColor: 'var(--accent-soft-strong)',
  },
  '.cm-selectionMatch': {backgroundColor: 'var(--accent-soft)'},
  '.cm-matchingBracket, &.cm-focused .cm-matchingBracket': {
    backgroundColor: 'var(--accent-soft)', outline: '1px solid var(--accent-soft-strong)',
  },
});

const highlight = HighlightStyle.define([
  {tag: [t.keyword, t.controlKeyword, t.moduleKeyword, t.operatorKeyword], color: 'var(--accent-strong)'},
  {tag: [t.string, t.special(t.string), t.regexp], color: 'var(--ok)'},
  {tag: [t.number, t.bool, t.null, t.atom], color: 'var(--warn)'},
  {tag: [t.comment, t.lineComment, t.blockComment, t.docComment], color: 'var(--text-subtle)', fontStyle: 'italic'},
  {tag: [t.function(t.variableName), t.function(t.propertyName)], color: 'var(--info)'},
  {tag: [t.typeName, t.className, t.namespace], color: 'var(--log-f)'},
  {tag: [t.operator, t.punctuation, t.bracket, t.separator], color: 'var(--text-muted)'},
  {tag: [t.propertyName, t.variableName, t.definition(t.variableName)], color: 'var(--text)'},
  {tag: t.invalid, color: 'var(--err)'},
]);

// CodeEditor is a thin React wrapper around CodeMirror 6 for viewing/editing
// Frida JS scripts. It owns one EditorView for its lifetime; external value
// changes (e.g. switching the selected script) replace the document, and edits
// are reported through onChange.
export function CodeEditor({value, onChange, readOnly}: {value: string; onChange?: (v: string) => void; readOnly?: boolean}) {
  const host = useRef<HTMLDivElement>(null);
  const view = useRef<EditorView | null>(null);
  const onChangeRef = useRef(onChange);
  onChangeRef.current = onChange;
  const editable = useRef(new Compartment());

  useEffect(() => {
    if (!host.current) return;
    const state = EditorState.create({
      doc: value,
      extensions: [
        basicSetup,
        javascript(),
        editorTheme,
        syntaxHighlighting(highlight),
        EditorView.lineWrapping,
        editable.current.of([EditorView.editable.of(!readOnly), EditorState.readOnly.of(!!readOnly)]),
        EditorView.updateListener.of(u => {
          if (u.docChanged && onChangeRef.current) onChangeRef.current(u.state.doc.toString());
        }),
      ],
    });
    const v = new EditorView({state, parent: host.current});
    view.current = v;
    return () => { v.destroy(); view.current = null; };
    // Editor is created once; value/readOnly are synced by the effects below.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // External document replacement (switching scripts, reverting edits).
  useEffect(() => {
    const v = view.current;
    if (!v) return;
    const cur = v.state.doc.toString();
    if (value !== cur) {
      v.dispatch({changes: {from: 0, to: cur.length, insert: value}});
    }
  }, [value]);

  // Toggle editability without rebuilding the editor.
  useEffect(() => {
    const v = view.current;
    if (!v) return;
    v.dispatch({effects: editable.current.reconfigure([EditorView.editable.of(!readOnly), EditorState.readOnly.of(!!readOnly)])});
  }, [readOnly]);

  return <div ref={host} className='code-editor' style={{height: '100%', minHeight: 0, overflow: 'hidden', border: '1px solid var(--border)', borderRadius: 6}}/>;
}

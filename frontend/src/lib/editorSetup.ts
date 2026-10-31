import { Compartment, EditorState, type Extension } from '@codemirror/state'
import {
  EditorView,
  crosshairCursor,
  drawSelection,
  dropCursor,
  highlightActiveLine,
  highlightActiveLineGutter,
  highlightSpecialChars,
  keymap,
  lineNumbers,
  rectangularSelection,
  ViewPlugin,
} from '@codemirror/view'
import { defaultKeymap, history, historyKeymap, indentWithTab } from '@codemirror/commands'
import {
  HighlightStyle,
  bracketMatching,
  foldGutter,
  foldKeymap,
  indentOnInput,
  syntaxHighlighting,
} from '@codemirror/language'
import { highlightSelectionMatches, searchKeymap } from '@codemirror/search'
import { autocompletion, closeBrackets, closeBracketsKeymap, completionKeymap } from '@codemirror/autocomplete'
import { tags as t } from '@lezer/highlight'
import { useSettings } from '../store/settings'

const theme = EditorView.theme(
  {
    '&': { height: '100%', color: 'var(--fg)', backgroundColor: 'var(--bg-editor)', fontSize: 'var(--editor-font-size)' },
    '.cm-scroller': { fontFamily: 'var(--editor-font)', lineHeight: 'var(--editor-line-height)' },
    '.cm-content': { caretColor: 'var(--accent)' },
    '.cm-cursor, .cm-dropCursor': { borderLeftColor: 'var(--accent)', borderLeftWidth: '2px' },
    '&.cm-focused .cm-selectionBackground, .cm-selectionBackground, ::selection': {
      backgroundColor: 'var(--selection) !important',
    },
    '.cm-gutters': { backgroundColor: 'var(--bg-editor)', color: 'var(--fg-faint)', border: 'none' },
    '.cm-activeLineGutter': { backgroundColor: 'transparent', color: 'var(--fg-muted)' },
    '.cm-activeLine': { backgroundColor: 'var(--active-line)' },
    '.cm-selectionMatch': { backgroundColor: 'var(--selection-match)' },
    '.cm-searchMatch': { backgroundColor: 'var(--search-match)', outline: '1px solid var(--accent-dim)' },
    '.cm-searchMatch-selected': { backgroundColor: 'var(--accent-dim)' },
    '&.cm-focused .cm-matchingBracket': { backgroundColor: 'var(--selection-match)', outline: 'none' },
    '.cm-tooltip': { backgroundColor: 'var(--bg-raised)', border: '1px solid var(--border)', color: 'var(--fg)' },
    '.cm-tooltip-autocomplete > ul > li[aria-selected]': { backgroundColor: 'var(--accent-dim)', color: 'var(--fg)' },
    '.cm-panels': { backgroundColor: 'var(--bg-raised)', color: 'var(--fg)' },
    '.cm-panels input, .cm-panels button': { fontSize: '12px' },
    '.cm-foldPlaceholder': { backgroundColor: 'var(--bg-raised)', border: 'none', color: 'var(--fg-muted)' },
    '.cm-flash-line': { backgroundColor: 'var(--flash)' },
  },
)

// Colors and fonts are CSS variables (style.css, store/settings), so a settings
// change restyles every editor by itself. What CSS can't do is done here for each
// live editor: switch CodeMirror's own light/dark base theme, and re-measure lines
// after a font change.
const darkMode = new Compartment()
const liveViews = new Set<EditorView>()
const tracked = ViewPlugin.define((view) => {
  liveViews.add(view)
  return { destroy: () => liveViews.delete(view) }
})

useSettings.subscribe((s, prev) => {
  const themeChanged = s.theme !== prev.theme
  if (!themeChanged && s.settings.editor === prev.settings.editor) return
  for (const view of liveViews) {
    if (themeChanged) view.dispatch({ effects: darkMode.reconfigure(EditorView.darkTheme.of(s.theme === 'dark')) })
    view.requestMeasure()
  }
})

const highlight = HighlightStyle.define([
  { tag: [t.keyword, t.modifier, t.operatorKeyword, t.controlKeyword], color: 'var(--syn-keyword)' },
  { tag: [t.string, t.special(t.string), t.regexp], color: 'var(--syn-string)' },
  { tag: [t.number, t.bool, t.null, t.atom], color: 'var(--syn-number)' },
  { tag: [t.comment, t.lineComment, t.blockComment, t.docComment], color: 'var(--syn-comment)', fontStyle: 'italic' },
  { tag: [t.function(t.variableName), t.function(t.propertyName)], color: 'var(--syn-function)' },
  { tag: [t.typeName, t.className, t.namespace, t.standard(t.typeName)], color: 'var(--syn-type)' },
  { tag: [t.propertyName, t.attributeName], color: 'var(--syn-property)' },
  { tag: [t.definition(t.variableName), t.labelName], color: 'var(--syn-definition)' },
  { tag: [t.operator, t.punctuation, t.bracket], color: 'var(--syn-punct)' },
  { tag: [t.heading], color: 'var(--syn-keyword)', fontWeight: 'bold' },
  { tag: [t.link, t.url], color: 'var(--syn-string)', textDecoration: 'underline' },
  { tag: t.emphasis, fontStyle: 'italic' },
  { tag: t.strong, fontWeight: 'bold' },
  { tag: t.invalid, color: 'var(--danger)' },
])

/** Base editor extensions: a trimmed-down basicSetup with Flint's theme. */
export function baseExtensions(): Extension[] {
  return [
    lineNumbers(),
    highlightActiveLineGutter(),
    highlightSpecialChars(),
    history(),
    foldGutter(),
    drawSelection(),
    dropCursor(),
    EditorState.allowMultipleSelections.of(true),
    indentOnInput(),
    syntaxHighlighting(highlight),
    bracketMatching(),
    closeBrackets(),
    autocompletion(),
    rectangularSelection(),
    crosshairCursor(),
    highlightActiveLine(),
    highlightSelectionMatches(),
    keymap.of([
      ...closeBracketsKeymap,
      ...defaultKeymap,
      ...searchKeymap,
      ...historyKeymap,
      ...foldKeymap,
      ...completionKeymap,
      indentWithTab,
    ]),
    theme,
    darkMode.of(EditorView.darkTheme.of(useSettings.getState().theme === 'dark')),
    tracked,
  ]
}

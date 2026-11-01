// The one Esc binding of the editor's AI features, so their order is explicit:
// a shown suggestion is dismissed first; only then does Esc close the inline
// edit prompt (and stop a running agent). Framework-free so it is testable.

/** A key handler: true when it took the key. */
export type KeyHandler<V> = (view: V) => boolean

/** Esc: dismiss the suggestion if one is shown, else close the inline edit. */
export function escapeKey<V>(dismissSuggestion: KeyHandler<V>, closeInlineEdit: KeyHandler<V>): KeyHandler<V> {
  return (view) => dismissSuggestion(view) || closeInlineEdit(view)
}

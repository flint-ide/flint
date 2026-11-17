import { copyHookSnippet } from '../../store/terminalAgent'

/** Settings ▸ Agents ▸ Claude in a terminal tab: hooks that make the tab show what Claude is doing. */
export function TerminalAgentSection() {
  return (
    <>
      <h3>Claude in a terminal tab</h3>
      <p className="muted settings-note">
        The command New Claude Terminal starts <code>claude</code> with a settings file of Flint's own (in Flint's config folder), whose hooks
        make the tab show working, waiting or done and list the agent in the Agents panel. Flint does not change your Claude Code settings.
        If you would rather have these hooks everywhere, copy them and merge them into the "hooks" of your own settings. Whether Claude Code merges
        hooks from <code>--settings</code> with your own has not been verified.
      </p>
      <div className="settings-actions">
        <button onClick={() => void copyHookSnippet()}>Copy hook snippet</button>
      </div>
    </>
  )
}

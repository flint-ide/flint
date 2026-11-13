// The editor's right-click menu (IntelliJ-like): navigation, clipboard, running the
// file, the agent, git and the file itself. Entries are existing commands, so the menu
// shows what the palette would and runs the same code; pure, so it is testable.
import type { DropdownItem } from '../components/ui/Dropdown'

/** A command as the menu shows it; null when it isn't available here. */
export interface EditorMenuCommand {
  title: string
  kbd?: string
}

export interface EditorMenuInput {
  hasSelection: boolean
  command: (id: string) => EditorMenuCommand | null
  runCommand: (id: string) => void
  clipboard: { cut: () => void; copy: () => void; paste: () => void; selectAll: () => void }
  clipboardKeys?: { cut?: string; copy?: string; paste?: string; selectAll?: string }
  /** Run / debug of the file's program (▶ next to func main), when it has one. */
  program: DropdownItem[]
  /** Path actions; null for an editor without a file on disk. */
  file: { copyPath: () => void; copyRelativePath: () => void; reveal: () => void } | null
}

const NAVIGATION = ['nav.goToDeclaration', 'nav.goToImplementation', 'nav.findUsages']
const REFACTOR = ['editor.rename', 'editor.quickFix', 'editor.format']
// run.context ("Run Test at Cursor") only makes sense in a test file, i.e. when test.file is available.
const TESTS = ['run.context', 'test.file', 'debug.testsInFile', 'debug.file']
const AGENT = ['agent.inlineEdit', 'agent.sendSelection', 'agent.fixFileProblems']
const GIT = ['git.blame', 'git.closeBlame', 'git.fileHistory']

export function editorContextMenu(input: EditorMenuInput): DropdownItem[] {
  const commands = (ids: string[]): DropdownItem[] =>
    ids.flatMap((id) => {
      const cmd = input.command(id)
      return cmd ? [{ key: id, label: cmd.title, kbd: cmd.kbd, onSelect: () => input.runCommand(id) }] : []
    })
  const { clipboard: c, clipboardKeys: k = {} } = input
  const tests = input.command('test.file') ? TESTS : TESTS.filter((id) => id !== 'run.context')
  const groups: DropdownItem[][] = [
    commands(NAVIGATION),
    commands(REFACTOR),
    [
      { key: 'cut', label: 'Cut', kbd: k.cut, disabled: !input.hasSelection, onSelect: c.cut },
      { key: 'copy', label: 'Copy', kbd: k.copy, disabled: !input.hasSelection, onSelect: c.copy },
      { key: 'paste', label: 'Paste', kbd: k.paste, onSelect: c.paste },
      { key: 'selectAll', label: 'Select All', kbd: k.selectAll, onSelect: c.selectAll },
    ],
    [...input.program, ...commands(tests)],
    commands(AGENT),
    commands(GIT),
    input.file
      ? [
          { key: 'copyPath', label: 'Copy Path', onSelect: input.file.copyPath },
          { key: 'copyRelativePath', label: 'Copy Relative Path', onSelect: input.file.copyRelativePath },
          { key: 'reveal', label: 'Reveal in Finder', onSelect: input.file.reveal },
        ]
      : [],
  ]
  return groups
    .filter((g) => g.length > 0)
    .flatMap((g, i): DropdownItem[] => (i === 0 ? g : [{ type: 'separator' }, ...g]))
}

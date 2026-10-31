// What dragging folders and files from Finder into the window does, pure so it
// is testable: folders become projects, files open in the editor.

export interface DroppedPath {
  path: string
  dir: boolean
}

export interface DropPlan {
  /** Folder to open as the project (only when none is open yet). */
  open?: string
  /** Folders to attach to the workspace. */
  add: string[]
  /** Files to open in tabs. */
  files: string[]
}

/** Dropping a whole folder of files must not open hundreds of tabs. */
export const MAX_DROPPED_FILES = 20

export function planDrop(dropped: readonly DroppedPath[], roots: readonly string[]): DropPlan {
  const dirs = dropped.filter((d) => d.dir && !roots.includes(d.path)).map((d) => d.path)
  const files = dropped.filter((d) => !d.dir).map((d) => d.path).slice(0, MAX_DROPPED_FILES)
  if (roots.length === 0 && dirs.length > 0) return { open: dirs[0], add: dirs.slice(1), files }
  return { add: dirs, files }
}

// Right click on a test marker in the editor gutter: Run, Debug and Profile for the
// test (or the whole file / package at the file marker), like IntelliJ's gutter menu.
import type { DropdownItem } from '../components/ui/Dropdown'
import { testLang, type TestMark } from '../lib/testFiles'
import { openContextMenu } from './contextMenu'
import { debugTestMark } from './debug'
import { profileTestAt, profileTestsInFile, profileTestsInPackage } from './profile'
import { runTestAt, runTestsInFile, runTestsInPackage } from './run'

export function testMarkMenu(path: string, mark: TestMark, x: number, y: number): void {
  openContextMenu(x, y, testMarkItems(path, mark), { minWidth: 240 })
}

function testMarkItems(path: string, mark: TestMark): DropdownItem[] {
  const go = testLang(path) === 'go'
  const file = path.slice(path.lastIndexOf('/') + 1)
  if (mark.kind === 'file') {
    return [
      { label: `Run Tests in ${file}`, onSelect: () => void runTestsInFile(path) },
      { label: go ? 'Run Package Tests' : 'Run All Tests', onSelect: () => void runTestsInPackage(path) },
      ...(go
        ? ([
            { label: `Debug Tests in ${file}`, onSelect: () => debugTestMark(path, mark) },
            { type: 'separator' },
            { label: `Profile Tests in ${file}`, hint: 'CPU + memory', onSelect: () => void profileTestsInFile(path) },
            { label: 'Profile Package Tests', onSelect: () => void profileTestsInPackage(path) },
          ] satisfies DropdownItem[])
        : []),
    ]
  }
  return [
    { label: `Run ${mark.name}`, onSelect: () => void runTestAt(path, mark.name) },
    ...(go
      ? ([
          { label: `Debug ${mark.name}`, onSelect: () => debugTestMark(path, mark) },
          { label: `Profile ${mark.name}`, hint: 'CPU + memory', onSelect: () => void profileTestAt(path, mark.name) },
        ] satisfies DropdownItem[])
      : []),
  ]
}

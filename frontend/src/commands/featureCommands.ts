// Commands of the run/test, debugger, git history, GitHub, HTTP client and "fix with agent" features. Kept apart
// from appCommands so each feature's actions read as one list.
import { registerCommands, type Command } from '../lib/commands'
import { isMarkdown } from '../lib/previewFiles'
import { markdownMode, setMarkdownMode } from '../store/preview'
import { testLang } from '../lib/testFiles'
import { useEditor, viewFor } from '../store/editor'
import { useGit } from '../store/git'
import { activeFilePath, showFileHistory, toggleBlame, useGitHistory } from '../store/gitHistory'
import { openStashDialog } from '../store/stash'
import { showToast } from '../store/dialog'
import { useWorkspace } from '../store/workspace'
import { checkoutPullRequest, detailsKey, loadDetails, loadPullRequests, openInBrowser, usePullRequests } from '../store/pullRequests'
import {
  activeHttpFile,
  clearCookies,
  copyRequestAsCurl,
  newHttpFile,
  pickEnvironment,
  setFollowRedirects,
  useHttpClient,
} from '../store/httpClient'
import {
  canDebug,
  debugSelected,
  debugStep,
  debugTestsInFile,
  isDebugging,
  isPaused,
  setAllBreakpoints,
  stopDebug,
  toggleBreakpointAtCursor,
  useDebug,
} from '../store/debug'
import {
  cancelProfiling,
  isProfiling,
  openProfileFile,
  profilable,
  profileSelected,
  profileTestsInFile,
  profileTestsInPackage,
} from '../store/profile'
import {
  configKey,
  editRunConfig,
  failedTests,
  fixFailedTestsWithAgent,
  newRunConfig,
  rerun,
  rerunFailedTests,
  runContext,
  runSelected,
  runTestsInFile,
  runTestsInPackage,
  selectedRunConfig,
  stopRun,
  useRun,
} from '../store/run'

// ⌃R in a shell is reverse history search; leave it there.
const IN_TERMINAL = '.terminal-view'

const isRepo = () => useGit.getState().isRepo
const filePath = () => activeFilePath()
const testFile = () => {
  const path = filePath()
  return !!path && !!testLang(path)
}
const running = () => Object.values(useRun.getState().sessions).some((s) => s.running)
const hasFailures = () => failedTests().length > 0

export function registerFeatureCommands(): () => void {
  // Debug last: while debugging, its keys (⌘F2, F8) win over Run's and the editor's.
  return registerCommands([
    ...previewCommands(),
    ...runCommands(),
    ...profileCommands(),
    ...gitCommands(),
    ...githubCommands(),
    ...agentCommands(),
    ...httpCommands(),
    ...debugCommands(),
  ])
}

function githubCommands(): Command[] {
  // The pull request of the active tab (its overview or one of its files).
  const activePullRequest = () => {
    const { tabs, activeId } = useEditor.getState()
    const tab = tabs.find((t) => t.id === activeId)
    if (tab?.kind !== 'pr' && tab?.kind !== 'prdiff') return null
    const data = usePullRequests.getState().details[detailsKey(tab.root, tab.number)]?.data
    return { root: tab.root, number: tab.number, data }
  }
  const panelShown = () => {
    const { sidebar, sidebarVisible } = useWorkspace.getState()
    return sidebarVisible && sidebar === 'pulls'
  }
  // "Pull Requests…" itself comes from the panel registry (panels/index.ts).
  return [
    {
      id: 'github.refresh', title: 'Refresh Pull Requests', category: 'GitHub', keywords: 'pr reload gh',
      when: () => panelShown() || !!activePullRequest(),
      run: () => {
        const pr = activePullRequest()
        if (pr) void loadDetails(pr.root, pr.number, true)
        if (panelShown()) void loadPullRequests(usePullRequests.getState().root, true)
      },
    },
    {
      id: 'github.openInBrowser', title: 'Open Pull Request on GitHub', category: 'GitHub', keywords: 'pr browser web',
      when: () => !!activePullRequest()?.data, run: () => openInBrowser(activePullRequest()?.data?.url ?? ''),
    },
    {
      id: 'github.checkout', title: 'Check Out Pull Request', category: 'GitHub', keywords: 'pr branch gh pr checkout',
      when: () => activePullRequest()?.data?.state === 'OPEN',
      run: () => {
        const pr = activePullRequest()
        if (pr?.data) return checkoutPullRequest(pr.root, pr.number, pr.data.headRef)
      },
    },
  ]
}

function httpCommands(): Command[] {
  const httpEditor = () => {
    const path = activeHttpFile()
    const { activeId } = useEditor.getState()
    const view = activeId ? viewFor(activeId) : undefined
    return path && view ? { path, view } : null
  }
  // The .http file of the active tab: the file itself or its response tab.
  const httpPath = () => {
    const { tabs, activeId } = useEditor.getState()
    const tab = tabs.find((t) => t.id === activeId)
    return tab?.kind === 'http' ? tab.path : activeHttpFile()
  }
  return [
    {
      id: 'http.send', title: 'Send Request', category: 'HTTP Client', hintKeys: 'Mod+Enter', keywords: 'rest api run execute http',
      when: () => !!httpEditor(),
      run: async () => {
        const target = httpEditor()
        if (!target) return
        // Loaded with the .http editor, so already there.
        const { sendAtCursor } = await import('../lib/httpEditor')
        sendAtCursor(target.view, target.path)
      },
    },
    { id: 'http.newFile', title: 'New HTTP Request File', category: 'HTTP Client', keywords: 'rest api client scratch .http', run: () => newHttpFile() },
    {
      id: 'http.selectEnv', title: 'Select HTTP Environment', category: 'HTTP Client', keywords: 'env variables dev prod http-client.env.json',
      when: () => !!httpPath(),
      run: () => {
        const path = httpPath()
        if (!path) return
        // Under the environment button of the visible editor, else near the top of the window.
        const anchor = document.querySelector('.tab-panel:not([hidden]) .http-bar .select')?.getBoundingClientRect()
        return pickEnvironment(path, anchor?.left ?? window.innerWidth / 2 - 100, anchor ? anchor.bottom + 2 : 80)
      },
    },
    {
      id: 'http.copyCurl', title: 'Copy Request as curl', category: 'HTTP Client', keywords: 'http shell command',
      when: () => !!httpEditor(),
      run: () => {
        const target = httpEditor()
        if (!target) return
        const { state } = target.view
        return copyRequestAsCurl(target.path, state.doc.toString(), state.doc.lineAt(state.selection.main.head).number)
      },
    },
    {
      id: 'http.followRedirects', title: 'Toggle HTTP Follow Redirects', category: 'HTTP Client', keywords: '3xx location',
      when: () => !!httpPath(),
      run: () => {
        const follow = !useHttpClient.getState().followRedirects
        setFollowRedirects(follow)
        showToast(follow ? 'HTTP client follows redirects' : 'HTTP client stops at redirects')
      },
    },
    { id: 'http.clearCookies', title: 'Clear HTTP Cookies', category: 'HTTP Client', keywords: 'cookie jar session', when: () => !!httpPath(), run: () => clearCookies(httpPath()!) },
  ]
}

function previewCommands(): Command[] {
  const mdFile = () => {
    const path = filePath()
    return path && isMarkdown(path) ? path : null
  }
  return [
    {
      id: 'preview.toggleMarkdown', title: 'Toggle Markdown Preview', category: 'View', keys: 'Mod+Shift+V', keywords: 'md render',
      when: () => !!mdFile(), run: () => setMarkdownMode(mdFile()!, markdownMode(mdFile()!) === 'preview' ? 'editor' : 'preview'),
    },
    {
      id: 'preview.markdownSide', title: 'Open Markdown Preview to the Side', category: 'View', keywords: 'md render split',
      when: () => !!mdFile(), run: () => setMarkdownMode(mdFile()!, 'split'),
    },
  ]
}

function runCommands(): Command[] {
  return [
    {
      id: 'run.selected', title: 'Run', category: 'Run', keys: 'Ctrl+R', keysIgnoredIn: IN_TERMINAL,
      keywords: 'start launch configuration', when: () => !!selectedRunConfig(), run: () => runSelected(),
    },
    {
      id: 'run.context', title: 'Run Test at Cursor', category: 'Run', keys: 'Ctrl+Shift+R', keysIgnoredIn: IN_TERMINAL,
      keywords: 'test context', run: () => runContext(),
    },
    { id: 'run.rerun', title: 'Rerun', category: 'Run', keywords: 'restart again', when: () => !!useRun.getState().lastKey, run: () => rerun() },
    { id: 'run.stop', title: 'Stop', category: 'Run', keys: 'Mod+F2', keywords: 'kill terminate', when: running, run: () => stopRun() },
    { id: 'run.edit', title: 'Edit Configurations…', category: 'Run', keywords: 'run.json settings', when: () => !!selectedRunConfig(), run: () => editRunConfig() },
    { id: 'run.new', title: 'Add Configuration…', category: 'Run', keywords: 'new run.json', run: () => newRunConfig() },
    { id: 'test.file', title: 'Run Tests in File', category: 'Run', when: testFile, run: () => runTestsInFile(filePath()!) },
    { id: 'test.package', title: 'Run Package Tests', category: 'Run', keywords: 'directory folder', when: testFile, run: () => runTestsInPackage(filePath()!) },
    { id: 'test.rerunFailed', title: 'Rerun Failed Tests', category: 'Run', when: hasFailures, run: () => rerunFailedTests() },
    {
      id: 'test.fixWithAgent', title: 'Fix Failed Tests with Agent', category: 'Agent', keywords: 'claude codex tests',
      when: hasFailures, run: () => fixFailedTestsWithAgent(),
    },
  ]
}

function profileCommands(): Command[] {
  const goTestFile = () => profilable(filePath())
  const selectedKey = () => {
    const { root } = useRun.getState()
    const config = selectedRunConfig()
    return root && config ? configKey(root, config.id) : null
  }
  return [
    {
      id: 'profile.testsInFile', title: 'Profile Tests in File', category: 'Profile', keywords: 'pprof cpu memory flame graph',
      when: goTestFile, run: () => profileTestsInFile(filePath()!),
    },
    {
      id: 'profile.package', title: 'Profile Package Tests', category: 'Profile', keywords: 'pprof cpu memory flame graph',
      when: goTestFile, run: () => profileTestsInPackage(filePath()!),
    },
    {
      // Go programs don't profile themselves: needs profileURL (net/http/pprof) in the configuration.
      id: 'profile.selected', title: 'Profile', category: 'Profile', keywords: 'pprof cpu flame graph configuration profileURL',
      when: () => !!selectedRunConfig() && !isProfiling(selectedKey() ?? undefined), run: () => profileSelected(),
    },
    { id: 'profile.cancel', title: 'Cancel Profiling', category: 'Profile', keywords: 'pprof stop', when: () => isProfiling(), run: () => cancelProfiling() },
    { id: 'profile.open', title: 'Open Profile File…', category: 'Profile', keywords: 'pprof flame graph .pprof', run: () => openProfileFile() },
  ]
}

function debugCommands(): Command[] {
  const goFile = () => !!filePath()?.endsWith('.go')
  return [
    {
      id: 'debug.selected', title: 'Debug', category: 'Debug', keys: 'Ctrl+D', keysIgnoredIn: IN_TERMINAL,
      keywords: 'dlv delve breakpoint launch', when: () => canDebug(), run: () => debugSelected(),
    },
    {
      id: 'debug.testsInFile', title: 'Debug Tests in File', category: 'Debug', keywords: 'dlv delve',
      when: () => testFile() && goFile(), run: () => debugTestsInFile(filePath()!),
    },
    { id: 'debug.resume', title: 'Resume Program', category: 'Debug', keys: 'F9', keywords: 'continue', when: isPaused, run: () => debugStep('continue') },
    { id: 'debug.stepOver', title: 'Step Over', category: 'Debug', keys: 'F8', keywords: 'next', when: isPaused, run: () => debugStep('next') },
    { id: 'debug.stepInto', title: 'Step Into', category: 'Debug', keys: 'F7', when: isPaused, run: () => debugStep('stepIn') },
    { id: 'debug.stepOut', title: 'Step Out', category: 'Debug', keys: 'Shift+F8', when: isPaused, run: () => debugStep('stepOut') },
    {
      id: 'debug.pause', title: 'Pause Program', category: 'Debug', keywords: 'break',
      when: () => useDebug.getState().session?.status === 'running', run: () => debugStep('pause'),
    },
    { id: 'debug.stop', title: 'Stop Debugging', category: 'Debug', keys: 'Mod+F2', keywords: 'kill terminate', when: isDebugging, run: () => stopDebug() },
    {
      id: 'debug.toggleBreakpoint', title: 'Toggle Line Breakpoint', category: 'Debug', keys: 'Mod+F8',
      when: goFile, run: () => toggleBreakpointAtCursor(),
    },
    {
      id: 'debug.clearBreakpoints', title: 'Remove All Breakpoints', category: 'Debug',
      when: () => Object.keys(useDebug.getState().breakpoints).length > 0, run: () => setAllBreakpoints({}),
    },
  ]
}

function gitCommands(): Command[] {
  return [
    {
      id: 'git.blame', title: 'Annotate with Git Blame', category: 'Git', keywords: 'blame annotate author',
      when: () => isRepo() && !!filePath() && !useGitHistory.getState().blame[filePath()!], run: () => toggleBlame(),
    },
    {
      id: 'git.closeBlame', title: 'Close Annotations', category: 'Git', keywords: 'blame',
      when: () => !!filePath() && !!useGitHistory.getState().blame[filePath()!], run: () => toggleBlame(),
    },
    { id: 'git.fileHistory', title: 'Show File History', category: 'Git', keywords: 'log commits', when: () => isRepo() && !!filePath(), run: () => showFileHistory() },
    { id: 'git.stash', title: 'Stash Changes…', category: 'Git', keywords: 'shelve', when: isRepo, run: () => openStashDialog() },
  ]
}

function agentCommands(): Command[] {
  return [
    {
      id: 'agent.fixFileProblems', title: 'Fix Problems in File with Agent', category: 'Agent', hintKeys: 'Mod+Alt+Shift+L',
      keywords: 'lsp diagnostics errors claude codex', when: () => !!filePath(),
      run: async () => {
        const { activeId } = useEditor.getState()
        const view = activeId ? viewFor(activeId) : undefined
        const path = filePath()
        if (!view || !path) return
        // lspAgent lives with the lazily loaded editor code.
        const { fixFileProblemsWithAgent } = await import('../lib/lspAgent')
        fixFileProblemsWithAgent(view, path)
      },
    },
  ]
}

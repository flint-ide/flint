// Signed-in accounts (Go: api_accounts.go, internal/accounts). Signing in is
// GitHub's device flow: Go asks for a code, the user enters it on GitHub's
// page, Go waits for the token and keeps it in the system keyring. No token
// ever reaches this side: only the account's login, avatar, host and scopes.
import { createStore } from '../lib/store'
import * as AccountsAPI from '../../bindings/flint/accountsapi'
import type { ProviderInfo, SignInCode } from '../../bindings/flint/models'
import { errorMessage } from '../api/backend'
import { ClipboardSetText, EventsOn } from '../wails/runtime'
import { showToast } from './dialog'

export type { ProviderInfo, SignInCode }

/** A sign-in in progress (one at a time). */
export interface SignInState {
  provider: string
  /** starting: asking for a code; code: waiting for the user to enter it. */
  phase: 'starting' | 'code'
  code: SignInCode | null
}

interface AccountsState {
  providers: ProviderInfo[]
  loaded: boolean
  signIn: SignInState | null
  /** The last sign-in's error, shown where it was started. */
  error: string | null
  /** The Sign in with GitHub dialog (opened where a feature needs it). */
  dialog: boolean
  busy: boolean
}

export const useAccounts = createStore<AccountsState>(() => ({
  providers: [],
  loaded: false,
  signIn: null,
  error: null,
  dialog: false,
  busy: false,
}))

const set = useAccounts.setState
const get = useAccounts.getState

export const GITHUB = 'github'

/** The provider's info ('github'). */
export function useProvider(id = GITHUB): ProviderInfo | null {
  return useAccounts((s) => s.providers.find((p) => p.id === id) ?? null)
}

export async function loadAccounts(): Promise<void> {
  try {
    set({ providers: (await AccountsAPI.Providers()) ?? [], loaded: true })
  } catch (err) {
    set({ loaded: true, error: errorMessage(err) })
  }
}

/** Runs whenever an account signs in or out (in this window or by Go). */
export function onAccountsChanged(callback: () => void): () => void {
  return EventsOn('accounts:changed', callback)
}

let waiting: { id: string; cancel: () => void } | null = null

/**
 * Signs in: shows the code, then waits (in Go) until it was entered on the
 * provider's page. Resolves to whether an account was added.
 */
export async function signIn(provider = GITHUB): Promise<boolean> {
  cancelSignIn()
  set({ signIn: { provider, phase: 'starting', code: null }, error: null })
  let code: SignInCode
  try {
    code = await AccountsAPI.StartSignIn(provider)
  } catch (err) {
    set({ signIn: null, error: errorMessage(err) })
    return false
  }
  set({ signIn: { provider, phase: 'code', code } })
  const finish = AccountsAPI.FinishSignIn(code.id)
  waiting = { id: code.id, cancel: () => finish.cancel() }
  try {
    const account = await finish
    showToast(`Signed in to ${account.host} as ${account.login}`)
    set({ signIn: null, dialog: false })
    return true
  } catch (err) {
    if (get().signIn?.code?.id === code.id) set({ signIn: null, error: errorMessage(err) })
    return false
  } finally {
    if (waiting?.id === code.id) waiting = null
    void loadAccounts()
  }
}

/** Copies the code and opens the provider's page (Go opens the URL it was sent). */
export async function openSignInPage(): Promise<void> {
  const code = get().signIn?.code
  if (!code) return
  await ClipboardSetText(code.userCode)
  try {
    await AccountsAPI.OpenSignInPage(code.id)
  } catch (err) {
    showToast(errorMessage(err), 'error')
  }
}

export function cancelSignIn(): void {
  const code = get().signIn?.code
  waiting?.cancel()
  waiting = null
  if (code) void AccountsAPI.CancelSignIn(code.id).catch(() => {})
  set({ signIn: null })
}

export async function signOut(provider = GITHUB): Promise<void> {
  try {
    await AccountsAPI.SignOut(provider)
    showToast('Signed out: the token was deleted from the system keyring')
  } catch (err) {
    showToast(errorMessage(err), 'error')
  }
  await loadAccounts()
}

/** Stores the GitHub CLI's token (Go asks first in a native dialog). */
export async function importGitHubCLI(): Promise<boolean> {
  set({ busy: true, error: null })
  try {
    const account = await AccountsAPI.ImportGitHubCLI()
    showToast(`Using the GitHub CLI's token: signed in to ${account.host} as ${account.login}`)
    set({ dialog: false })
    return true
  } catch (err) {
    const msg = errorMessage(err)
    if (!msg.includes('cancelled')) set({ error: msg })
    return false
  } finally {
    set({ busy: false })
    void loadAccounts()
  }
}

export function openAuthorizedApps(): void {
  AccountsAPI.OpenAuthorizedApps().catch((err) => showToast(errorMessage(err), 'error'))
}

/** Opens the Sign in with GitHub dialog (a feature found no account). */
export function requestGitHubSignIn(): void {
  set({ dialog: true, error: null })
  void loadAccounts()
}

export function closeSignInDialog(): void {
  cancelSignIn()
  set({ dialog: false })
}

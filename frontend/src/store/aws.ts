import { createStore } from '../lib/store'
import * as AWSAPI from '../../bindings/flint/awsapi'
import { errorMessage } from '../api/backend'
import { profileEnv, shellCommand, validProfileName, type AwsProfile } from '../lib/aws'
import { useTerminal } from './terminal'
import { activeRoot } from './activeRoot'
import { showToast } from './dialog'

export interface AwsIdentity {
  account: string
  arn: string
}

interface AwsState {
  profiles: AwsProfile[]
  loaded: boolean
  loading: boolean
  error: string | null
  query: string
  /** Who each checked profile is (sts get-caller-identity), or why it failed. */
  identities: Record<string, AwsIdentity | { error: string } | 'checking'>
}

export const useAws = createStore<AwsState>(() => ({
  profiles: [], loaded: false, loading: false, error: null, query: '', identities: {},
}))

let pending: Promise<void> | null = null

/** Re-reads ~/.aws (cheap: two small files and the SSO token cache). */
export function refreshAws(): Promise<void> {
  pending ??= (async () => {
    useAws.setState({ loading: true })
    try {
      const profiles = (await AWSAPI.Profiles()) ?? []
      useAws.setState({ profiles, loaded: true, error: null })
    } catch (err) {
      useAws.setState({ error: errorMessage(err), loaded: true })
    } finally {
      useAws.setState({ loading: false })
      pending = null
    }
  })()
  return pending
}

export const setAwsQuery = (query: string) => useAws.setState({ query })

function setIdentity(name: string, value: AwsState['identities'][string]): void {
  useAws.setState((s) => ({ identities: { ...s.identities, [name]: value } }))
}

/** Asks STS who the profile is; the answer shows under the profile. */
export async function checkIdentity(name: string): Promise<void> {
  setIdentity(name, 'checking')
  try {
    const id = await AWSAPI.Identity(name)
    setIdentity(name, { account: id.account, arn: id.arn })
  } catch (err) {
    setIdentity(name, { error: errorMessage(err) })
  }
}

/** A terminal working as the profile (AWS_PROFILE, AWS_REGION), named "aws:<profile> · …". */
export function openProfileTerminal(p: AwsProfile): void {
  if (!validProfileName(p.name)) return showToast(`Invalid profile name ${p.name}`, 'error')
  useTerminal.getState().create(activeRoot(), { env: profileEnv(p), tag: `aws:${p.name}` })
}

/** Runs the profile's login command (aws sso login, saml2aws…) in a terminal; the list refreshes when Flint gets focus back. */
export function loginProfile(p: AwsProfile): void {
  if (!p.login?.length || !validProfileName(p.name)) return
  useTerminal.getState().create(activeRoot(), { title: `aws login: ${p.name}`, input: shellCommand(p.login), env: profileEnv(p) })
}

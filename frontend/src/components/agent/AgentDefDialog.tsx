import { useEffect, useMemo, useState } from 'react'
import { Alert, Button, Form, FormActions, Input, OptionRow, OptionRowGroup } from '@flint-ide/ui'
import { trustedRoots, validAgentName } from '../../lib/agentDefs'
import { rootLabels } from '../../lib/workspaceRoots'
import { activeRoot } from '../../store/activeRoot'
import { closeAgentDefDialog, createAgentDef } from '../../store/agentDefs'
import { useAgentChat } from '../../store/agentChat'
import { useTrust } from '../../store/trust'
import { useWorkspace } from '../../store/workspace'
import { Modal } from '../layout/Modal'
import { Select } from '../ui/Select'

/**
 * New Agent Definition…: names an agent and creates its Markdown file from a template
 * in the config folder (personal) or in .flint/agents of a trusted workspace folder.
 */
export function AgentDefDialog() {
  const roots = useWorkspace((s) => s.roots)
  const trusted = useTrust((s) => s.trusted)
  const projects = useMemo(() => trustedRoots(roots, trusted), [roots, trusted])
  const labels = useMemo(() => rootLabels(projects), [projects])
  const [name, setName] = useState('')
  const [personal, setPersonal] = useState(projects.length === 0)
  const [root, setRoot] = useState(() => (projects.includes(activeRoot()) ? activeRoot() : (projects[0] ?? '')))
  const [error, setError] = useState('')
  // Definitions are files: read them again, so a taken name shows up before Go says so.
  useEffect(() => {
    void useAgentChat.getState().load()
  }, [])
  const valid = validAgentName(name)
  const create = async () => {
    if (!valid) return
    const err = await createAgentDef(personal ? '' : root, personal, name)
    if (err) return setError(err)
    void useAgentChat.getState().load()
    closeAgentDefDialog()
  }
  return (
    <Modal title="New Agent" onClose={closeAgentDefDialog} className="new-prompt">
      <Form
        onSubmit={(e) => {
          e.preventDefault()
          void create()
        }}
      >
        <Input
          autoFocus
          label="Name (used wherever an agent name is)"
          value={name}
          spellCheck={false}
          placeholder="reviewer"
          error={name && !valid ? 'Lowercase letters, digits and - only, starting with a letter (at most 32).' : undefined}
          onChange={(e) => {
            setName(e.target.value.trim())
            setError('')
          }}
        />
        <OptionRowGroup value={personal ? 'personal' : 'repository'} onValueChange={(v) => setPersonal(v === 'personal')} aria-label="Where to create the agent">
          <OptionRow
            value="repository"
            disabled={projects.length === 0}
            title="Repository"
            description={projects.length === 0 ? 'Open and trust a folder to create a repository agent.' : '.flint/agents, shared with the team through the repository'}
          />
          <OptionRow value="personal" title="Personal" description="Only you, in every folder" />
        </OptionRowGroup>
        {!personal && projects.length > 1 && <Select block value={root} options={projects.map((r, i) => ({ value: r, label: labels[i] ?? r }))} onChange={setRoot} />}
        {error && <Alert size="inline" variant="negative">{error}</Alert>}
        <FormActions align="start">
          <Button type="button" variant="plain" onClick={closeAgentDefDialog}>Cancel</Button>
          <Button type="submit" variant="primary" disabled={!valid}>Create and Open</Button>
        </FormActions>
      </Form>
    </Modal>
  )
}

import { Component, type ReactNode } from 'react'

interface Props {
  children: ReactNode
  /** Short label of what failed, e.g. the tab title. */
  label?: string
}

interface State {
  error: Error | null
}

/** Contains a crash (or a failed lazy chunk) to one pane instead of blanking the app. */
export class ErrorBoundary extends Component<Props, State> {
  state: State = { error: null }

  static getDerivedStateFromError(error: Error): State {
    return { error }
  }

  render() {
    if (!this.state.error) return this.props.children
    return (
      <div className="placeholder error selectable">
        <div>{this.props.label ? `${this.props.label} failed to load.` : 'Something went wrong.'}</div>
        <code>{this.state.error.message}</code>
        <button onClick={() => this.setState({ error: null })}>Retry</button>
      </div>
    )
  }
}

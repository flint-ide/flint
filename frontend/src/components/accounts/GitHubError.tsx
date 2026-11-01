import { isSignInError } from '../../lib/githubSettings'
import { SignInWithGitHub } from './SignInWithGitHub'

/** An error from a GitHub call: Sign in with GitHub when it means "no account", else the message. */
export function GitHubError({ message, className = 'placeholder' }: { message: string; className?: string }) {
  if (isSignInError(message)) {
    return (
      <div className={className}>
        <SignInWithGitHub intro="Not signed in to GitHub." />
      </div>
    )
  }
  return <div className={`${className} error selectable`}>{message}</div>
}

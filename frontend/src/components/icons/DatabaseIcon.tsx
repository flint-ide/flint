/** Cylinder icon; databases the user can't open get pgAdmin's red cross. */
export function DatabaseIcon({ locked }: { locked?: boolean }) {
  return (
    <svg className={`db-icon${locked ? ' locked' : ''}`} width="14" height="14" viewBox="0 0 14 14" aria-hidden>
      <ellipse cx="6" cy="3.5" rx="4.5" ry="1.8" fill="currentColor" opacity="0.9" />
      <path d="M1.5 3.5v6c0 1 2 1.8 4.5 1.8s4.5-.8 4.5-1.8v-6c0 1-2 1.8-4.5 1.8S1.5 4.5 1.5 3.5z" fill="currentColor" opacity="0.55" />
      {locked && <path d="M9 9l4 4M13 9l-4 4" stroke="#ef4444" strokeWidth="1.8" strokeLinecap="round" />}
    </svg>
  )
}

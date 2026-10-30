import { useDialog } from '../../store/dialog'
import { Modal } from './Modal'

/** Global confirm dialog and toast notification. */
export function Overlays() {
  const confirm = useDialog((s) => s.confirm)
  const toast = useDialog((s) => s.toast)

  return (
    <>
      {confirm && (
        <Modal onClose={() => confirm.resolve(false)} className="confirm">
          <p>{confirm.message}</p>
          <div className="form-actions">
            <span className="spacer" />
            <button onClick={() => confirm.resolve(false)}>Cancel</button>
            <button className="danger" autoFocus onClick={() => confirm.resolve(true)}>
              {confirm.confirmLabel}
            </button>
          </div>
        </Modal>
      )}
      {toast && <div className={`toast ${toast.kind}`}>{toast.message}</div>}
    </>
  )
}

// The parts of the Wails runtime Flint uses, with the v2 call shapes: event
// handlers get the emitted value itself (v3 wraps it in a WailsEvent).
import { Browser, Clipboard, Events, Window } from '@wailsio/runtime'

/** Subscribes to a backend event; returns the unsubscribe function. */
export function EventsOn(name: string, callback: (data: any) => void): () => void {
  return Events.On(name, (event) => callback(event.data))
}

export const BrowserOpenURL = (url: string): void => void Browser.OpenURL(url)

/** Resolves to whether the text was copied. */
export const ClipboardSetText = (text: string): Promise<boolean> =>
  Clipboard.SetText(text).then(
    () => true,
    () => false,
  )

/** The clipboard's text ('' when empty or unreadable). */
export const ClipboardGetText = (): Promise<string> => Clipboard.Text().catch(() => '')

export const WindowToggleMaximise = (): Promise<void> => Window.ToggleMaximise()

export const WindowIsFullscreen = (): Promise<boolean> => Window.IsFullscreen()

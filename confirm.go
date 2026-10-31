//go:build !server

package main

import "time"

// confirmWait bounds how long a binding waits for a confirmation dialog.
const confirmWait = 10 * time.Minute

// askNative shows a native question over the main window and reports
// whether the user chose ok. Go shows it and reads the click, so no code in
// the web view can answer or skip it. Cancel is the default button: Return
// doesn't run anything.
func (a *App) askNative(title, message, ok string) bool {
	if a.wails == nil {
		return false
	}
	answer := make(chan bool, 1)
	d := a.wails.Dialog.Question().SetTitle(title).SetMessage(message)
	d.AddButton(ok).OnClick(func() { answer <- true })
	cancel := d.AddButton("Cancel").OnClick(func() { answer <- false })
	d.SetDefaultButton(cancel)
	d.SetCancelButton(cancel)
	if a.window != nil {
		d.AttachToWindow(a.window)
	}
	d.Show()
	var quit <-chan struct{}
	if a.ctx != nil {
		quit = a.ctx.Done()
	}
	select {
	case yes := <-answer:
		return yes
	case <-quit:
	case <-time.After(confirmWait):
	}
	return false
}

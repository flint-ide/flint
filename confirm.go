//go:build !server

package main

import "time"

// confirmWait bounds how long a binding waits for a confirmation dialog.
const confirmWait = 10 * time.Minute

// askNativeAnswered shows a native question over the main window and
// reports whether the user chose ok, and whether they answered at all (false
// when nobody did within confirmWait or Flint quit). Go shows it and reads the
// click, so no code in the web view can answer or skip it. Cancel is the
// default button: Return doesn't run anything.
func (a *App) askNativeAnswered(title, message, ok string) (yes, answered bool) {
	if a.wails == nil {
		return false, false
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
		return yes, true
	case <-quit:
	case <-time.After(confirmWait):
	}
	return false, false
}

// chooseNative shows a native question with buttons (plus Cancel, the
// default) and returns the button chosen, "" for Cancel.
func (a *App) chooseNative(title, message string, buttons []string) string {
	if a.wails == nil {
		return ""
	}
	answer := make(chan string, 1)
	d := a.wails.Dialog.Question().SetTitle(title).SetMessage(message)
	for _, b := range buttons {
		d.AddButton(b).OnClick(func() { answer <- b })
	}
	cancel := d.AddButton("Cancel").OnClick(func() { answer <- "" })
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
	case b := <-answer:
		return b
	case <-quit:
	case <-time.After(confirmWait):
	}
	return ""
}

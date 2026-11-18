//go:build !server

package main

// askNative shows a native question over the main window and reports
// whether the user chose ok. Go shows it and reads the click, so no code in
// the web view can answer or skip it. Cancel is the default button: Return
// doesn't run anything. It waits as long as the dialog is on screen: a
// dialog can't be closed from code, so it stays until the user answers it,
// or Flint quits (false then). An asker that can't wait that long gives up
// on its own (askUserCtx) and ignores the late answer.
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
	}
	return false
}

// chooseNative shows a native question with buttons (plus Cancel, the
// default) and returns the button chosen, "" for Cancel; it waits like
// askNative.
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
	}
	return ""
}

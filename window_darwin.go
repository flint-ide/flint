//go:build darwin

package main

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework Cocoa
#import <Cocoa/Cocoa.h>

// flintStyleWindows switches the (Wails-created) toolbar to the compact unified
// style used by Xcode and Safari, so the native traffic lights sit in a 38pt bar
// that matches Flint's own title bar instead of a tall 52pt one.
static void flintStyleWindows(void) {
	dispatch_async(dispatch_get_main_queue(), ^{
		for (NSWindow *w in [NSApp windows]) {
			if (@available(macOS 11.0, *)) {
				w.toolbarStyle = NSWindowToolbarStyleUnifiedCompact;
				w.titlebarSeparatorStyle = NSTitlebarSeparatorStyleNone;
			}
		}
	});
}

// flintTitlebarMetrics reports the title bar height and the close button's frame
// (in points, origin top-left) so the web title bar can line up with them.
static void flintTitlebarMetrics(double *height, double *x, double *y, double *w, double *h) {
	__block double hh = 0, bx = 0, by = 0, bw = 0, bh = 0;
	dispatch_sync(dispatch_get_main_queue(), ^{
		NSWindow *win = [NSApp mainWindow] ?: [[NSApp windows] firstObject];
		if (!win) return;
		hh = win.frame.size.height - win.contentLayoutRect.size.height;
		NSButton *close = [win standardWindowButton:NSWindowCloseButton];
		if (close) {
			NSRect f = [close convertRect:close.bounds toView:nil];
			bx = f.origin.x;
			bw = f.size.width;
			bh = f.size.height;
			by = win.frame.size.height - f.origin.y - f.size.height;
		}
	});
	*height = hh; *x = bx; *y = by; *w = bw; *h = bh;
}
*/
import "C"

// styleNativeWindow applies the compact toolbar style; call once the window exists.
func styleNativeWindow() { C.flintStyleWindows() }

// TitlebarMetrics describes where macOS draws the window buttons.
type TitlebarMetrics struct {
	Height       float64 `json:"height"`       // title bar height in points
	ButtonsLeft  float64 `json:"buttonsLeft"`  // x of the close button
	ButtonsTop   float64 `json:"buttonsTop"`   // y of the close button from the top
	ButtonWidth  float64 `json:"buttonWidth"`  // size of one traffic light
	ButtonHeight float64 `json:"buttonHeight"` //
}

func titlebarMetrics() TitlebarMetrics {
	var h, x, y, w, bh C.double
	C.flintTitlebarMetrics(&h, &x, &y, &w, &bh)
	return TitlebarMetrics{Height: float64(h), ButtonsLeft: float64(x), ButtonsTop: float64(y), ButtonWidth: float64(w), ButtonHeight: float64(bh)}
}

//go:build darwin && !server

package main

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework Cocoa
#import <Cocoa/Cocoa.h>

// flintSetAppearance switches the native window chrome (title bar, traffic
// lights, menus) to dark (1), light (2) or the system appearance (0) and reports
// whether the result is dark.
static int flintSetAppearance(int mode) {
	__block int dark = 1;
	dispatch_sync(dispatch_get_main_queue(), ^{
		NSAppearance *a = nil;
		if (mode == 1) a = [NSAppearance appearanceNamed:NSAppearanceNameDarkAqua];
		if (mode == 2) a = [NSAppearance appearanceNamed:NSAppearanceNameAqua];
		NSApp.appearance = a;
		NSAppearanceName best = [NSApp.effectiveAppearance bestMatchFromAppearancesWithNames:@[NSAppearanceNameAqua, NSAppearanceNameDarkAqua]];
		dark = [best isEqualToString:NSAppearanceNameDarkAqua];
	});
	return dark;
}

// flintSystemPrefersDark reads the macOS appearance without needing NSApp (it is
// called before the app starts).
static int flintSystemPrefersDark(void) {
	NSString *style = [[NSUserDefaults standardUserDefaults] stringForKey:@"AppleInterfaceStyle"];
	return style != nil && [style caseInsensitiveCompare:@"Dark"] == NSOrderedSame;
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

// flintFindWebView finds the window's WKWebView (Wails puts it in the content
// view).
static NSView *flintFindWebView(NSView *view) {
	if ([view isKindOfClass:NSClassFromString(@"WKWebView")]) return view;
	for (NSView *sub in view.subviews) {
		NSView *found = flintFindWebView(sub);
		if (found) return found;
	}
	return nil;
}

// flintSetWebViewHidden hides or shows the window's web view. Main thread only.
static void flintSetWebViewHidden(void *nsWindow, int hidden) {
	NSWindow *win = (__bridge NSWindow *)nsWindow;
	flintFindWebView(win.contentView).hidden = hidden != 0;
}
*/
import "C"

import (
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

// onLaunched runs launched once macOS has finished launching the app, and
// reveals the web view when its page has loaded.
func (a *App) onLaunched(app *application.App, win *application.WebviewWindow, launched func()) {
	app.Event.OnApplicationEvent(events.Mac.ApplicationDidFinishLaunching, func(*application.ApplicationEvent) { launched() })
	win.OnWindowEvent(events.Mac.WebViewDidFinishNavigation, func(*application.WindowEvent) { a.onPageLoaded() })
}

// linuxWindow: no Linux options on macOS.
func linuxWindow() application.LinuxWindow { return application.LinuxWindow{} }

// setNativeAppearance makes the window chrome follow theme ("dark", "light" or
// "system") and reports whether it is now dark. Not for the main thread.
func setNativeAppearance(theme string) bool {
	mode := C.int(0)
	switch theme {
	case "dark":
		mode = 1
	case "light":
		mode = 2
	}
	return C.flintSetAppearance(mode) != 0
}

// systemPrefersDark reports whether macOS uses the dark appearance.
func systemPrefersDark() bool { return C.flintSystemPrefersDark() != 0 }

func titlebarMetrics() TitlebarMetrics {
	var h, x, y, w, bh C.double
	C.flintTitlebarMetrics(&h, &x, &y, &w, &bh)
	return TitlebarMetrics{Height: float64(h), ButtonsLeft: float64(x), ButtonsTop: float64(y), ButtonWidth: float64(w), ButtonHeight: float64(bh)}
}

// showWindowEarly shows the window as soon as it exists. Wails shows it only
// once the page has loaded, which leaves the screen empty while WebKit starts
// its processes; this way the title bar and the theme's background appear at
// once. The web view stays hidden until the page is loaded (onPageLoaded), so
// it never flashes white.
func (a *App) showWindowEarly() {
	created := false
	application.InvokeSync(func() {
		w := a.window.NativeWindow()
		if w == nil {
			return // not created yet: onPageLoaded shows it
		}
		if !a.pageLoaded.Load() {
			C.flintSetWebViewHidden(w, 1)
		}
		created = true
	})
	if created {
		a.window.Show()
	}
}

// onPageLoaded reveals the web view once the page has loaded and shows the
// window, as Wails does for a window that isn't created hidden.
func (a *App) onPageLoaded() {
	a.pageLoaded.Store(true)
	application.InvokeSync(func() {
		if w := a.window.NativeWindow(); w != nil {
			C.flintSetWebViewHidden(w, 0)
		}
	})
	a.window.Show()
}

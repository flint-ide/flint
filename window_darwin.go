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

// The app menu's "Settings…" item, added next to the items Wails creates.
extern void flintMenuOpenSettings(void);

@interface FlintMenuTarget : NSObject
- (void)openSettings:(id)sender;
@end

@implementation FlintMenuTarget
- (void)openSettings:(id)sender { flintMenuOpenSettings(); }
@end

static const NSInteger flintSettingsTag = 0xF11E7;

static void flintAddSettingsItem(void) {
	dispatch_async(dispatch_get_main_queue(), ^{
		static FlintMenuTarget *target;
		NSMenu *appMenu = [[[NSApp mainMenu] itemAtIndex:0] submenu];
		if (!appMenu || [appMenu itemWithTag:flintSettingsTag]) return;
		if (!target) target = [FlintMenuTarget new];
		NSMenuItem *item = [[NSMenuItem alloc] initWithTitle:@"Settings…" action:@selector(openSettings:) keyEquivalent:@","];
		item.target = target;
		item.tag = flintSettingsTag;
		// After "About flint" and its separator, as in other Mac apps.
		NSInteger at = appMenu.numberOfItems >= 2 && [[appMenu itemAtIndex:1] isSeparatorItem] ? 2 : 0;
		[appMenu insertItem:item atIndex:at];
		[appMenu insertItem:[NSMenuItem separatorItem] atIndex:at + 1];
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

// addSettingsMenuItem puts "Settings… ⌘," into the app menu; call after the
// menu is (re)installed. Adding it twice is a no-op.
func addSettingsMenuItem() { C.flintAddSettingsItem() }

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

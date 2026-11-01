package main

/*
#cgo LDFLAGS: -framework CoreGraphics -framework CoreFoundation
#include <CoreGraphics/CoreGraphics.h>

// hasWindow reports whether pid owns an on-screen, normal-level window of a
// real size (not a menu bar item or a tooltip). Reading owners and bounds
// needs no Screen Recording permission.
static int hasWindow(int pid) {
	CFArrayRef list = CGWindowListCopyWindowInfo(kCGWindowListOptionOnScreenOnly | kCGWindowListExcludeDesktopElements, kCGNullWindowID);
	if (!list) return 0;
	int found = 0;
	for (CFIndex i = 0; i < CFArrayGetCount(list) && !found; i++) {
		CFDictionaryRef w = CFArrayGetValueAtIndex(list, i);
		int owner = 0, layer = -1;
		CFNumberGetValue(CFDictionaryGetValue(w, kCGWindowOwnerPID), kCFNumberIntType, &owner);
		CFNumberGetValue(CFDictionaryGetValue(w, kCGWindowLayer), kCFNumberIntType, &layer);
		if (owner != pid || layer != 0) continue;
		CGRect r;
		if (CGRectMakeWithDictionaryRepresentation(CFDictionaryGetValue(w, kCGWindowBounds), &r) && r.size.width >= 300 && r.size.height >= 200) found = 1;
	}
	CFRelease(list);
	return found;
}
*/
import "C"

func hasWindow(pid int) bool { return C.hasWindow(C.int(pid)) == 1 }

//go:build darwin

package workspace

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework Foundation
#import <Foundation/Foundation.h>
#include <stdlib.h>

// flintTrash moves a file or folder to the user's Trash (undoable from Finder), returning
// NULL on success or a malloc'd error message.
static char *flintTrash(const char *path) {
	@autoreleasepool {
		NSURL *url = [NSURL fileURLWithPath:[NSString stringWithUTF8String:path]];
		NSError *err = nil;
		if ([[NSFileManager defaultManager] trashItemAtURL:url resultingItemURL:nil error:&err]) {
			return NULL;
		}
		return strdup(err.localizedDescription.UTF8String ?: "unknown error");
	}
}
*/
import "C"

import (
	"errors"
	"unsafe"
)

// MoveToTrash moves path to the macOS Trash with NSFileManager, so it can be put back.
func MoveToTrash(path string) error {
	cpath := C.CString(path)
	defer C.free(unsafe.Pointer(cpath))
	if msg := C.flintTrash(cpath); msg != nil {
		defer C.free(unsafe.Pointer(msg))
		return errors.New(C.GoString(msg))
	}
	return nil
}

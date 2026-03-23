package main

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Cocoa
#import <Cocoa/Cocoa.h>

void moveWindowToBottomRight() {
	NSScreen *screen = [NSScreen mainScreen];
	NSRect screenFrame = [screen visibleFrame];
	NSArray *windows = [NSApp windows];
	for (NSWindow *window in windows) {
		if ([window isVisible]) {
			NSRect windowFrame = [window frame];
			NSPoint origin;
			origin.x = screenFrame.origin.x + screenFrame.size.width - windowFrame.size.width;
			origin.y = screenFrame.origin.y;
			[window setFrameOrigin:origin];
			[window setLevel:NSFloatingWindowLevel];
			break;
		}
	}
}
*/
import "C"

func moveToBottomRight() {
	C.moveWindowToBottomRight()
}

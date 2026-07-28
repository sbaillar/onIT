#import <AppKit/AppKit.h>

// Fyne has no window-position API — fyne.Window exposes only CenterOnScreen,
// and the glfw backend that does have one lives in an internal package.
// Underneath it is an NSWindow, so ask AppKit directly.
//
// Only the origin is carried, never the size: the window is fixed-size and
// its height follows its content, so restoring a stale size would fight it.

// onitWindowOrigin writes the bottom-left corner of the window with this
// title into x/y and returns 1 when it found one.
int onitWindowOrigin(const char *title, double *x, double *y) {
	NSString *want = [NSString stringWithUTF8String:title];
	for (NSWindow *w in [NSApp windows]) {
		if ([[w title] isEqualToString:want]) {
			NSRect f = [w frame];
			*x = f.origin.x;
			*y = f.origin.y;
			return 1;
		}
	}
	return 0;
}

// onitSetWindowOrigin moves the window, but only if the target still lands on
// an attached screen — a position saved on a monitor since unplugged would
// otherwise put the window out of reach.
int onitSetWindowOrigin(const char *title, double x, double y) {
	NSString *want = [NSString stringWithUTF8String:title];
	for (NSWindow *w in [NSApp windows]) {
		if ([[w title] isEqualToString:want]) {
			NSRect f = [w frame];
			NSRect target = NSMakeRect(x, y, f.size.width, f.size.height);
			for (NSScreen *s in [NSScreen screens]) {
				if (NSIntersectsRect([s visibleFrame], target)) {
					[w setFrameOrigin:NSMakePoint(x, y)];
					return 1;
				}
			}
			return 0;
		}
	}
	return 0;
}

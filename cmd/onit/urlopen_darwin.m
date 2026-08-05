#import <AppKit/AppKit.h>

// The macOS widget opens onit:// on tap. When the app is already running the
// URL arrives as a kAEGetURL Apple Event; handling it means "bring up the
// window". (A cold launch never reaches the handler in time — LaunchServices
// starts the app, which shows its window by default anyway.)

extern void onitURLOpened(void);

@interface OnitURLHandler : NSObject
- (void)handleGetURL:(NSAppleEventDescriptor *)event
           withReply:(NSAppleEventDescriptor *)reply;
@end

@implementation OnitURLHandler
- (void)handleGetURL:(NSAppleEventDescriptor *)event
           withReply:(NSAppleEventDescriptor *)reply {
	(void)event;
	(void)reply;
	onitURLOpened();
}
@end

// registered once from main; the manager keeps a strong reference target
static OnitURLHandler *handler;

void onitRegisterURLHandler(void) {
	handler = [OnitURLHandler new];
	[[NSAppleEventManager sharedAppleEventManager]
	    setEventHandler:handler
	        andSelector:@selector(handleGetURL:withReply:)
	      forEventClass:kInternetEventClass
	         andEventID:kAEGetURL];
}

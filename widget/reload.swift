// onit-widgetreload: poke WidgetKit to re-render the onIT widget.
// WidgetCenter has no ObjC surface the Go app could reach with cgo, so the
// app execs this helper on every state change; WidgetKit coalesces bursts
// on its side. Embedded at onIT.app/Contents/MacOS/onit-widgetreload.

import WidgetKit

WidgetCenter.shared.reloadAllTimelines()
// The reload rides an async XPC message; exiting immediately can drop it.
RunLoop.main.run(until: Date(timeIntervalSinceNow: 0.5))

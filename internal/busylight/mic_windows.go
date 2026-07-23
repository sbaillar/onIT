package busylight

import (
	"golang.org/x/sys/windows/registry"
)

const consentStore = `Software\Microsoft\Windows\CurrentVersion\CapabilityAccessManager\ConsentStore\microphone`

// micInUse reports whether any app is capturing from the microphone:
// Windows tracks per-app usage under the ConsentStore, and a zero
// LastUsedTimeStop with a nonzero start means "live right now".
func micInUse() bool {
	for _, root := range []string{consentStore + `\NonPackaged`, consentStore} {
		k, err := registry.OpenKey(registry.CURRENT_USER, root, registry.READ)
		if err != nil {
			continue
		}
		names, err := k.ReadSubKeyNames(0)
		k.Close()
		if err != nil {
			continue
		}
		for _, name := range names {
			sub, err := registry.OpenKey(registry.CURRENT_USER, root+`\`+name, registry.READ)
			if err != nil {
				continue
			}
			start, _, err1 := sub.GetIntegerValue("LastUsedTimeStart")
			stop, _, err2 := sub.GetIntegerValue("LastUsedTimeStop")
			sub.Close()
			if err1 == nil && err2 == nil && start > 0 && stop == 0 {
				return true
			}
		}
	}
	return false
}

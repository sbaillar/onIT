//go:build !darwin

package recorder

// Supported: this build has a capture backend.
const Supported = false

func captureStart() (float64, error) { return 0, ErrUnsupported }
func captureRead([]float32) int      { return 0 }
func captureStop()                   {}
func captureDropped() int            { return 0 }

//go:build !darwin

package main

// onit:// URLs are a macOS widget affordance; nothing to register elsewhere.
func registerURLHandler(func()) {}

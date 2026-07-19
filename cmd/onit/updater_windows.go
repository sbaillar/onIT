package main

// No-op: the Windows flow reveals a zip the user extracts by hand, and
// launching the new exe already starts a fresh process whose
// single-instance takeover stops the old one.
func relaunchWhenInstalled(string) {}

// Package power keeps the computer awake while a measurement runs, so that
// standby does not interrupt the evidence.
package power

// Inhibitor keeps the system awake until Release is called.
type Inhibitor interface{ Release() }

// KeepAwake prevents system sleep (the display may still turn off).
func KeepAwake() Inhibitor { return keepAwake() }

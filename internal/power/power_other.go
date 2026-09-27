//go:build !windows

package power

type noop struct{}

func (noop) Release() {}

func keepAwake() Inhibitor { return noop{} }

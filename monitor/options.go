package monitor

import "time"

// WithProbeTimeout specifies a timeout after which the monitor starts the fall back procedure
func (sm *StartupMonitor) WithProbeTimeout(timeout time.Duration) *StartupMonitor {
	sm.timeout = timeout
	return sm
}

// WithProbeInterval probeInterval specifies a time interval at which health of the target will be assessed.
// Be mindful of not setting it too low, on each iteration, an i/o is involved
func (sm *StartupMonitor) WithProbeInterval(probeInterval time.Duration) *StartupMonitor {
	sm.probeInterval = probeInterval
	return sm
}

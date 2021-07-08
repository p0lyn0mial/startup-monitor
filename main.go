package main

import (
	"context"
	"github.com/p0lyn0mial/startup-monitor/monitor"
	"time"
)

func main() {
	// register sigterm/sigint signals
	shutdownCtx := monitor.SetupSignalContext(context.TODO())

	// start monitor
	sm := monitor.New(nil).
		WithProbeResponseTimeout(5 * time.Second).
		WithProbeInterval(time.Second)
	sm.Run(shutdownCtx)
}

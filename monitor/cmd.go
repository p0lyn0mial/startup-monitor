package monitor

import (
	"context"
	"fmt"
	"time"

	"github.com/davecgh/go-spew/spew"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"k8s.io/klog/v2"
)

type StartupMonitorOptions struct {
	// FallbackTimeout specifies a timeout after which the monitor starts the fall back procedure
	FallbackTimeout time.Duration

	// IsTargetHealthy defines a function that abstracts away assessing operand's health condition.
	// This is the extention point for the operators to provide a custom health function for their operands
	IsTargetHealthy func() bool
}

func NewStartupMonitorCommand() *cobra.Command {
	o := StartupMonitorOptions{}

	cmd := &cobra.Command{
		Use:   "startup-monitor",
		Short: "Monitors the provided static pod revision and if it proves unhealthy rolls back to the previous revision.",
		Run: func(cmd *cobra.Command, args []string) {
			klog.V(1).Info(cmd.Flags())
			klog.V(1).Info(spew.Sdump(o))

			if err := o.Validate(); err != nil {
				klog.Exit(err)
			}

			o.Run()
		},
	}

	o.AddFlags(cmd.Flags())

	return cmd
}

func (o *StartupMonitorOptions) AddFlags(fs *pflag.FlagSet) {
	fs.DurationVar(&o.FallbackTimeout, "fallback-timeout-duration", 120*time.Second, "maximum time in seconds to wait for the operand to become healthy (default: 2m)")
}

func (o *StartupMonitorOptions) Validate() error {
	if o.FallbackTimeout == 0 {
		return fmt.Errorf("--fallback-timeout-duration cannot be 0")
	}
	return nil
}

func (o *StartupMonitorOptions) Run() {
	shutdownCtx := SetupSignalContext(context.TODO())

	// start monitor
	sm := New(nil).
		WithProbeTimeout(o.FallbackTimeout).
		WithProbeInterval(time.Second)

	sm.Run(shutdownCtx)
}

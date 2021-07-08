package monitor

import (
	"context"

	"k8s.io/apiserver/pkg/server"
	"k8s.io/klog/v2"
)

// SetupSignalContext registers for SIGTERM and SIGINT and returns a context
// that will be cancelled once a signal is received.
func SetupSignalContext(baseCtx context.Context) context.Context {
	shutdownCtx, cancel := context.WithCancel(baseCtx)
	shutdownHandler := server.SetupSignalHandler()
	go func() {
		defer cancel()
		<-shutdownHandler
		klog.Infof("Received SIGTERM or SIGINT signal, shutting down the process.")
	}()
	return shutdownCtx
}

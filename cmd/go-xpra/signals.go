package main

import (
	"os"
	"os/signal"
	"syscall"
)

// interruptSignals are the signals that end a session gracefully: Ctrl-C, the
// terminal going away, and the polite termination request a service manager
// sends. Only os.Interrupt is delivered on Windows; naming the others there is
// harmless.
var interruptSignals = []os.Signal{os.Interrupt, syscall.SIGTERM, syscall.SIGHUP}

// onInterrupt calls shutdown, on its own goroutine, the first time the process
// is interrupted, and then stops handling signals so that a second interrupt
// kills the process the ordinary way: a shutdown that stalls on a dead
// connection must still be interruptible.
//
// The returned function uninstalls the handler and must be called when the
// work being protected is over.
func onInterrupt(shutdown func(os.Signal)) func() {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, interruptSignals...)
	done := make(chan struct{})
	go func() {
		select {
		case caught := <-signals:
			signal.Stop(signals)
			shutdown(caught)
		case <-done:
		}
	}()
	return func() {
		signal.Stop(signals)
		close(done)
	}
}

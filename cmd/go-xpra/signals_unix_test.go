//go:build unix

package main

import (
	"os"
	"syscall"
	"testing"
	"time"
)

func TestOnInterruptRunsTheShutdown(t *testing.T) {
	caught := make(chan os.Signal, 1)
	stop := onInterrupt(func(signal os.Signal) { caught <- signal })
	defer stop()

	if err := syscall.Kill(syscall.Getpid(), syscall.SIGINT); err != nil {
		t.Fatalf("sending SIGINT to the test process: %v", err)
	}
	select {
	case signal := <-caught:
		if signal != syscall.SIGINT {
			t.Errorf("shutdown ran for %v, want SIGINT", signal)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for the shutdown to run")
	}
}

// Nothing must be left listening once the protected work is over, or a later
// interrupt would be swallowed instead of killing the process.
func TestOnInterruptStopsListening(t *testing.T) {
	ran := make(chan struct{}, 1)
	stop := onInterrupt(func(os.Signal) { ran <- struct{}{} })
	stop()

	// signal.Stop has returned, so a signal sent now goes to the default
	// handler; catch it here so that it does not kill the test process.
	guard := make(chan os.Signal, 1)
	restore := onInterrupt(func(signal os.Signal) { guard <- signal })
	defer restore()
	if err := syscall.Kill(syscall.Getpid(), syscall.SIGINT); err != nil {
		t.Fatalf("sending SIGINT to the test process: %v", err)
	}
	select {
	case <-guard:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for the signal")
	}
	select {
	case <-ran:
		t.Error("the stopped handler still ran its shutdown")
	default:
	}
}

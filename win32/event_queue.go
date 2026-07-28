package win32

import (
	"sync"

	"github.com/Xpra-org/go-xpra/ui"
)

// eventQueue lets the window thread hand events to a relay without blocking.
//
// maxPending is a soft limit: once it is reached, motion and configure events
// may be coalesced or discarded, and an important event may evict one of them.
// The queue grows only when it contains nothing safe to discard. In particular,
// key and button transitions must remain ordered and must never be lost, or the
// remote application can be left with an input held down indefinitely.
type eventQueue struct {
	mu         sync.Mutex
	pending    []ui.Event
	maxPending int
	wake       chan struct{}
}

func newEventQueue(maxPending int) *eventQueue {
	return &eventQueue{
		maxPending: maxPending,
		wake:       make(chan struct{}, 1),
	}
}

// push adds event without waiting for the consumer.
func (q *eventQueue) push(event ui.Event) {
	q.mu.Lock()
	added := q.pushLocked(event)
	q.mu.Unlock()

	if added {
		select {
		case q.wake <- struct{}{}:
		default:
		}
	}
}

func (q *eventQueue) pushLocked(event ui.Event) bool {
	if len(q.pending) < q.maxPending {
		q.pending = append(q.pending, event)
		return true
	}

	if isCoalescible(event) {
		// Keep the most recent geometry or pointer position for a window in
		// place of its stale predecessor. If there is no matching event, this
		// new coalescible event is the one safe thing to discard.
		for i := len(q.pending) - 1; i >= 0; i-- {
			if canCoalesce(q.pending[i], event) {
				q.pending[i] = event
				return true
			}
		}
		return false
	}

	// Make room for an input transition or another non-coalescible event by
	// sacrificing stale presentation state first.
	for i, queued := range q.pending {
		if isCoalescible(queued) {
			copy(q.pending[i:], q.pending[i+1:])
			q.pending[len(q.pending)-1] = event
			return true
		}
	}

	// A queue made entirely of important events has no safe victim. Allow it
	// to grow rather than losing a release and leaving remote input stuck.
	q.pending = append(q.pending, event)
	return true
}

func (q *eventQueue) pop() (ui.Event, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.pending) == 0 {
		return nil, false
	}
	event := q.pending[0]
	copy(q.pending, q.pending[1:])
	q.pending[len(q.pending)-1] = nil
	q.pending = q.pending[:len(q.pending)-1]
	return event, true
}

func isCoalescible(event ui.Event) bool {
	switch event.(type) {
	case ui.Configure, ui.Motion:
		return true
	default:
		return false
	}
}

func canCoalesce(old, next ui.Event) bool {
	switch next := next.(type) {
	case ui.Configure:
		old, ok := old.(ui.Configure)
		return ok && old.Window == next.Window
	case ui.Motion:
		old, ok := old.(ui.Motion)
		return ok && old.Window == next.Window
	default:
		return false
	}
}

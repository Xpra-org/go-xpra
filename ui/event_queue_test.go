package ui

import (
	"reflect"
	"testing"
)

func TestEventQueuePreservesReleasesAtCapacity(t *testing.T) {
	tests := []struct {
		name    string
		release Event
	}{
		{"key", Key{Window: 1, Name: "a", Pressed: false}},
		{"button", Button{Window: 1, Button: 1, Pressed: false}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			q := NewEventQueue(2)
			q.Push(Motion{Window: 1, X: 10, Y: 20})
			q.Push(Configure{Window: 1, Width: 800, Height: 600})
			q.Push(test.release)

			events := drainEventQueue(q)
			if len(events) != 2 {
				t.Fatalf("queue holds %d events, want 2", len(events))
			}
			if !reflect.DeepEqual(events[1], test.release) {
				t.Errorf("last event is %#v, want release %#v", events[1], test.release)
			}
		})
	}
}

func TestEventQueueGrowsRatherThanDroppingInputTransitions(t *testing.T) {
	q := NewEventQueue(1)
	press := Key{Window: 1, Name: "a", Pressed: true}
	release := Key{Window: 1, Name: "a", Pressed: false}
	q.Push(press)
	q.Push(release)

	events := drainEventQueue(q)
	if len(events) != 2 {
		t.Fatalf("queue holds %d events, want 2", len(events))
	}
	if !reflect.DeepEqual(events[0], press) || !reflect.DeepEqual(events[1], release) {
		t.Errorf("events are %#v, want press followed by release", events)
	}
}

func TestEventQueueCoalescesMotionAtCapacity(t *testing.T) {
	q := NewEventQueue(2)
	q.Push(Key{Window: 1, Name: "a", Pressed: true})
	q.Push(Motion{Window: 1, X: 10, Y: 20})
	q.Push(Motion{Window: 1, X: 30, Y: 40})

	events := drainEventQueue(q)
	if len(events) != 2 {
		t.Fatalf("queue holds %d events, want 2", len(events))
	}
	want := Motion{Window: 1, X: 30, Y: 40}
	if events[1] != want {
		t.Errorf("motion is %#v, want %#v", events[1], want)
	}
}

func TestEventQueueCoalescesClipboardAtCapacity(t *testing.T) {
	q := NewEventQueue(1)
	q.Push(ClipboardChange{Text: "old"})
	q.Push(ClipboardChange{Text: "new"})
	event, ok := q.Pop()
	if !ok || event != (ClipboardChange{Text: "new"}) {
		t.Fatalf("clipboard event = %#v, %v", event, ok)
	}
}

func drainEventQueue(q *EventQueue) []Event {
	var events []Event
	for {
		event, ok := q.Pop()
		if !ok {
			return events
		}
		events = append(events, event)
	}
}

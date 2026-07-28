package win32

import (
	"reflect"
	"testing"

	"github.com/Xpra-org/go-xpra/ui"
)

func TestEventQueuePreservesReleasesAtCapacity(t *testing.T) {
	tests := []struct {
		name    string
		release ui.Event
	}{
		{"key", ui.Key{Window: 1, Name: "a", Pressed: false}},
		{"button", ui.Button{Window: 1, Button: 1, Pressed: false}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			q := newEventQueue(2)
			q.push(ui.Motion{Window: 1, X: 10, Y: 20})
			q.push(ui.Configure{Window: 1, Width: 800, Height: 600})
			q.push(test.release)

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
	q := newEventQueue(1)
	press := ui.Key{Window: 1, Name: "a", Pressed: true}
	release := ui.Key{Window: 1, Name: "a", Pressed: false}
	q.push(press)
	q.push(release)

	events := drainEventQueue(q)
	if len(events) != 2 {
		t.Fatalf("queue holds %d events, want 2", len(events))
	}
	if !reflect.DeepEqual(events[0], press) || !reflect.DeepEqual(events[1], release) {
		t.Errorf("events are %#v, want press followed by release", events)
	}
}

func TestEventQueueCoalescesMotionAtCapacity(t *testing.T) {
	q := newEventQueue(2)
	q.push(ui.Key{Window: 1, Name: "a", Pressed: true})
	q.push(ui.Motion{Window: 1, X: 10, Y: 20})
	q.push(ui.Motion{Window: 1, X: 30, Y: 40})

	events := drainEventQueue(q)
	if len(events) != 2 {
		t.Fatalf("queue holds %d events, want 2", len(events))
	}
	want := ui.Motion{Window: 1, X: 30, Y: 40}
	if events[1] != want {
		t.Errorf("motion is %#v, want %#v", events[1], want)
	}
}

func drainEventQueue(q *eventQueue) []ui.Event {
	var events []ui.Event
	for {
		event, ok := q.pop()
		if !ok {
			return events
		}
		events = append(events, event)
	}
}

package workflows

import (
	"fmt"
	"testing"
)

// TestRoomForCounterKeepsChatsAtTheLimit: a full map of start counters
// forgets a chat below the limit, the one with the fewest starts, and never
// one that used its starts; when every chat is at the limit there is no room.
func TestRoomForCounterKeepsChatsAtTheLimit(t *testing.T) {
	counts := map[string]int{}
	for i := range maxStartCounters {
		counts[fmt.Sprint("c", i)] = maxStartsPerChat
	}
	counts["c7"] = 1
	counts["c8"] = 3
	if !roomForCounter(counts) {
		t.Fatal("no room though two chats are below the limit")
	}
	if _, ok := counts["c7"]; ok {
		t.Error("the chat with the fewest starts was kept")
	}
	if counts["c8"] != 3 || counts["c0"] != maxStartsPerChat || len(counts) != maxStartCounters-1 {
		t.Errorf("other counters changed: c8=%d c0=%d len=%d", counts["c8"], counts["c0"], len(counts))
	}
	counts["c7"] = maxStartsPerChat
	counts["c8"] = maxStartsPerChat
	if roomForCounter(counts) {
		t.Error("room made by forgetting a chat at the limit")
	}
	for i := range maxStartCounters {
		if counts[fmt.Sprint("c", i)] != maxStartsPerChat {
			t.Fatalf("chat c%d at the limit was forgotten", i)
		}
	}
	if !roomForCounter(map[string]int{"a": maxStartsPerChat}) {
		t.Error("no room in a map that is not full")
	}
}

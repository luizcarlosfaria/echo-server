package store

import (
	"slices"
	"sync"
	"testing"

	"github.com/luizcarlosfaria/echo-server/internal/model"
)

func ids(requests []*model.Request) []int64 {
	result := make([]int64, 0, len(requests))
	for _, r := range requests {
		result = append(result, r.ID)
	}
	return result
}

func TestOldestAreEvicted(t *testing.T) {
	s := New(5)
	for range 8 {
		s.Add(model.Request{})
	}
	if got := ids(s.Snapshot(0)); !slices.Equal(got, []int64{8, 7, 6, 5, 4}) {
		t.Fatalf("snapshot = %v", got)
	}
	if s.Count() != 5 || s.TotalReceived() != 8 || s.Capacity() != 5 {
		t.Fatalf("count=%d total=%d capacity=%d", s.Count(), s.TotalReceived(), s.Capacity())
	}
	if s.Get(3) != nil || s.Get(9) != nil || s.Get(0) != nil {
		t.Fatal("evicted or unknown ids must not be found")
	}
	for id := int64(4); id <= 8; id++ {
		if got := s.Get(id); got == nil || got.ID != id {
			t.Fatalf("Get(%d) = %v", id, got)
		}
	}
	if got := ids(s.Snapshot(2)); !slices.Equal(got, []int64{8, 7}) {
		t.Fatalf("limit = %v", got)
	}
}

func TestClearKeepsTheIdSequence(t *testing.T) {
	s := New(3)
	s.Add(model.Request{})
	s.Add(model.Request{})
	s.Clear()
	if s.Count() != 0 || len(s.Snapshot(0)) != 0 || s.Get(1) != nil {
		t.Fatal("clear must remove everything")
	}
	if got := s.Add(model.Request{}); got.ID != 3 {
		t.Fatalf("id after clear = %d, want 3", got.ID)
	}
}

func TestSubscribeIsAtomic(t *testing.T) {
	s := New(1000)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for range 500 {
			s.Add(model.Request{})
		}
	}()

	snapshot, total, events, unsubscribe := s.Subscribe(1000)
	defer unsubscribe()
	wg.Wait()

	// O snapshot e os eventos seguintes cobrem todos os ids, sem buracos nem duplicatas.
	seen := map[int64]bool{}
	for _, r := range snapshot {
		seen[r.ID] = true
	}
	if int64(len(snapshot)) != total {
		t.Fatalf("snapshot has %d items, totalReceived = %d", len(snapshot), total)
	}
	for id := total + 1; id <= 500; id++ {
		event := <-events
		if event.Type != EventRequest || event.Request.ID != id || seen[id] {
			t.Fatalf("event %+v, want request %d", event, id)
		}
	}
}

func TestSlowSubscriberDropsOldest(t *testing.T) {
	s := New(100)
	_, _, events, unsubscribe := s.Subscribe(3)
	defer unsubscribe()
	for range 10 {
		s.Add(model.Request{})
	}
	var got []int64
	for range 3 {
		got = append(got, (<-events).Request.ID)
	}
	if !slices.Equal(got, []int64{8, 9, 10}) {
		t.Fatalf("got %v, want the 3 newest", got)
	}
}

func TestUnsubscribe(t *testing.T) {
	s := New(10)
	_, _, events, unsubscribe := s.Subscribe(10)
	unsubscribe()
	unsubscribe() // idempotente
	s.Add(model.Request{})
	select {
	case e := <-events:
		t.Fatalf("unexpected event %+v", e)
	default:
	}
}

func TestConcurrentAdds(t *testing.T) {
	s := New(100)
	var wg sync.WaitGroup
	for range 50 {
		wg.Go(func() { s.Add(model.Request{}) })
	}
	wg.Wait()
	got := ids(s.Snapshot(0))
	slices.Sort(got)
	if len(slices.Compact(got)) != 50 {
		t.Fatalf("ids not unique: %v", got)
	}
}

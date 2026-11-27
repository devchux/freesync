package cache

import (
	"testing"
	"time"
)

func TestMountCacheRefreshDiscardsInFlightResult(t *testing.T) {
	cache := Cache[string, string]{ttl: time.Minute, limit: 2}
	started := make(chan struct{})
	release := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = cache.Load("directory", func() (string, error) {
			close(started)
			<-release
			return "old listing", nil
		})
	}()
	<-started
	cache.Clear()
	close(release)
	<-done

	got, err := cache.Load("directory", func() (string, error) {
		return "new listing", nil
	})
	if err != nil || got != "new listing" {
		t.Fatalf("load after refresh = %q, %v", got, err)
	}
}

func TestMountCacheReusesOnlyUnexpiredEntries(t *testing.T) {
	cache := Cache[string, int]{ttl: time.Minute, limit: 2}
	calls := 0
	fetch := func() (int, error) {
		calls++
		return calls, nil
	}
	_, _ = cache.Load("key", fetch)
	if got, _ := cache.Load("key", fetch); got != 1 || calls != 1 {
		t.Fatalf("warm load = %d with %d fetches", got, calls)
	}
	cache.entries["key"] = cacheEntry[int]{value: 1, until: time.Now().Add(-time.Second)}
	if got, _ := cache.Load("key", fetch); got != 2 || calls != 2 {
		t.Fatalf("expired load = %d with %d fetches", got, calls)
	}

	disabled := Cache[string, int]{}
	_, _ = disabled.Load("key", fetch)
	if got, _ := disabled.Load("key", fetch); got != 4 || calls != 4 {
		t.Fatalf("disabled cache load = %d with %d fetches", got, calls)
	}
}

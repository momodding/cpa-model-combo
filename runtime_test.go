package main

import (
	"math/rand/v2"
	"sync"
	"testing"
	"time"
)

func TestRouteRuntimePriorityCooldown(t *testing.T) {
	now := time.Date(2026, 8, 8, 0, 0, 0, 0, time.UTC)
	runtime := newRouteRuntime(func() time.Time { return now })
	route := testModelRoute("smart", routeStrategyPriority, 30, "provider-a", "provider-b")
	runtime.Sync([]modelRoute{route})

	if selected := runtime.Select(route); selected.model != "provider-a" {
		t.Fatalf("first selection = %#v", selected)
	}
	runtime.MarkFailure(route, "provider-a")
	if selected := runtime.Select(route); selected.model != "provider-b" {
		t.Fatalf("second selection = %#v", selected)
	}
	runtime.MarkFailure(route, "provider-b")
	if selected := runtime.Select(route); !selected.allCooling || selected.retryAfter != 30*time.Second {
		t.Fatalf("cooldown selection = %#v", selected)
	}

	now = now.Add(31 * time.Second)
	if selected := runtime.Select(route); selected.model != "provider-a" {
		t.Fatalf("selection after cooldown = %#v", selected)
	}
}

func TestRouteRuntimeRoundRobin(t *testing.T) {
	runtime := newRouteRuntime(nil)
	route := testModelRoute("smart", routeStrategyRoundRobin, 30, "a", "b")
	runtime.Sync([]modelRoute{route})
	want := []string{"a", "b", "a", "b"}
	for index, model := range want {
		if selected := runtime.Select(route); selected.model != model {
			t.Fatalf("selection %d = %#v, want %q", index, selected, model)
		}
	}
}

func TestRouteRuntimeWeightedRoundRobinUsesConsecutiveVirtualSlots(t *testing.T) {
	runtime := newRouteRuntime(nil)
	route := modelRoute{
		Alias:           "weighted",
		Strategy:        routeStrategyRoundRobin,
		CooldownSeconds: 30,
		Targets: []modelTarget{
			{Model: "mimo", Weight: 3},
			{Model: "deepseek", Weight: 1},
		},
	}
	runtime.Sync([]modelRoute{route})
	want := []string{"mimo", "mimo", "mimo", "deepseek", "mimo", "mimo", "mimo", "deepseek"}
	for index, model := range want {
		if selected := runtime.Select(route); selected.model != model {
			t.Fatalf("selection %d = %#v, want %q", index, selected, model)
		}
	}
}

func TestRouteRuntimeWeightedCooldownSkipsRemainingSlots(t *testing.T) {
	now := time.Date(2026, 8, 8, 0, 0, 0, 0, time.UTC)
	runtime := newRouteRuntime(func() time.Time { return now })
	route := modelRoute{
		Alias:           "weighted",
		Strategy:        routeStrategyRoundRobin,
		CooldownSeconds: 30,
		Targets: []modelTarget{
			{Model: "mimo", Weight: 3},
			{Model: "deepseek", Weight: 1},
		},
	}
	runtime.Sync([]modelRoute{route})
	if selected := runtime.Select(route); selected.model != "mimo" {
		t.Fatalf("initial selection = %#v", selected)
	}
	runtime.MarkFailure(route, "mimo")
	if selected := runtime.Select(route); selected.model != "deepseek" {
		t.Fatalf("selection after failed weighted target = %#v", selected)
	}
	runtime.MarkFailure(route, "deepseek")
	if selected := runtime.Select(route); !selected.allCooling || selected.retryAfter != 30*time.Second {
		t.Fatalf("all-cooling selection = %#v", selected)
	}
}

func TestRouteRuntimeExclusionsPreventRetryAfterCooldownExpires(t *testing.T) {
	now := time.Date(2026, 8, 8, 0, 0, 0, 0, time.UTC)
	runtime := newRouteRuntime(func() time.Time { return now })
	route := testModelRoute("weighted", routeStrategyRoundRobin, 1, "a", "b")
	runtime.Sync([]modelRoute{route})
	if selected := runtime.Select(route); selected.model != "a" {
		t.Fatalf("initial selection = %#v", selected)
	}
	runtime.MarkFailure(route, "a")
	now = now.Add(2 * time.Second)
	if selected := runtime.SelectExcluding(route, map[string]struct{}{"a": {}}); selected.model != "b" {
		t.Fatalf("selection after excluded cooldown expiry = %#v", selected)
	}
	if selected := runtime.Select(route); selected.model != "a" {
		t.Fatalf("unexcluded selection after cooldown expiry = %#v, want a", selected)
	}
}

func TestRouteRuntimePriorityIgnoresWeights(t *testing.T) {
	runtime := newRouteRuntime(nil)
	route := modelRoute{
		Alias:           "priority",
		Strategy:        routeStrategyPriority,
		CooldownSeconds: 30,
		Targets: []modelTarget{
			{Model: "first", Weight: 100},
			{Model: "second", Weight: 1},
		},
	}
	runtime.Sync([]modelRoute{route})
	for index := 0; index < 3; index++ {
		if selected := runtime.Select(route); selected.model != "first" {
			t.Fatalf("selection %d = %#v, want first", index, selected)
		}
	}
}

func TestRouteRuntimeCloneResetsCursorWhenWeightChanges(t *testing.T) {
	runtime := newRouteRuntime(nil)
	route := modelRoute{
		Alias:           "weighted",
		Strategy:        routeStrategyRoundRobin,
		CooldownSeconds: 30,
		Targets: []modelTarget{
			{Model: "first", Weight: 1},
			{Model: "second", Weight: 3},
		},
	}
	runtime.Sync([]modelRoute{route})
	if selected := runtime.Select(route); selected.model != "first" {
		t.Fatalf("initial selection = %#v", selected)
	}
	unchanged := runtime.Clone([]modelRoute{route})
	if selected := unchanged.Select(route); selected.model != "second" {
		t.Fatalf("unchanged clone selection = %#v", selected)
	}
	changed := route
	changed.Targets = []modelTarget{{Model: "first", Weight: 2}, {Model: "second", Weight: 3}}
	reset := runtime.Clone([]modelRoute{changed})
	if selected := reset.Select(changed); selected.model != "first" {
		t.Fatalf("changed clone selection = %#v, want first", selected)
	}
}

func TestRouteRuntimeClonePreservesOnlyUnchangedRoutes(t *testing.T) {
	now := time.Date(2026, 8, 8, 0, 0, 0, 0, time.UTC)
	runtime := newRouteRuntime(func() time.Time { return now })
	route := testModelRoute("smart", routeStrategyPriority, 30, "a", "b")
	runtime.Sync([]modelRoute{route})
	runtime.MarkFailure(route, "a")

	unchanged := runtime.Clone([]modelRoute{route})
	if selected := unchanged.Select(route); selected.model != "b" {
		t.Fatalf("unchanged clone selection = %#v", selected)
	}

	changedRoute := route
	changedRoute.Targets = []modelTarget{{Model: "a", Weight: defaultTargetWeight}, {Model: "c", Weight: defaultTargetWeight}}
	changed := runtime.Clone([]modelRoute{changedRoute})
	if selected := changed.Select(changedRoute); selected.model != "a" {
		t.Fatalf("changed clone selection = %#v", selected)
	}
}

type constSource struct{ value uint64 }

func (source constSource) Uint64() uint64 { return source.value }

func TestRouteRuntimeRandomSkipsExcludedAndCoolingTargets(t *testing.T) {
	now := time.Date(2026, 8, 8, 0, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	route := testModelRoute("smart", routeStrategyRandom, 30, "a", "b", "c", "d")
	excluded := map[string]struct{}{routeKey("a"): {}}
	newRuntime := func(source rand.Source) *routeRuntime {
		runtime := newRouteRuntimeWithRNG(clock, rand.New(source))
		runtime.Sync([]modelRoute{route})
		runtime.MarkFailure(route, "d")
		return runtime
	}
	// Eligible after exclusion and cooldown: [b, c]. Max draw picks last, zero draw picks first.
	if selected := newRuntime(constSource{value: ^uint64(0)}).SelectExcluding(route, excluded); selected.model != "c" {
		t.Fatalf("max draw selection = %#v, want c", selected)
	}
	if selected := newRuntime(constSource{value: 0}).SelectExcluding(route, excluded); selected.model != "b" {
		t.Fatalf("zero draw selection = %#v, want b", selected)
	}
	seeded := newRuntime(rand.NewPCG(7, 9))
	seen := map[string]int{}
	for range 200 {
		seen[seeded.SelectExcluding(route, excluded).model]++
	}
	if seen["a"] != 0 || seen["d"] != 0 || seen["b"] == 0 || seen["c"] == 0 {
		t.Fatalf("seeded selections = %#v, want only b and c", seen)
	}
}

func TestRouteRuntimeRandomIgnoresWeights(t *testing.T) {
	now := time.Date(2026, 8, 8, 0, 0, 0, 0, time.UTC)
	route := modelRoute{
		Alias:           "smart",
		Strategy:        routeStrategyRandom,
		CooldownSeconds: 30,
		Targets: []modelTarget{
			{Model: "light", Weight: 1},
			{Model: "heavy", Weight: 100},
		},
	}
	runtime := newRouteRuntimeWithRNG(func() time.Time { return now }, rand.New(rand.NewPCG(3, 5)))
	runtime.Sync([]modelRoute{route})
	counts := map[string]int{}
	for range 400 {
		counts[runtime.Select(route).model]++
	}
	if counts["light"] < 100 || counts["heavy"] == 0 {
		t.Fatalf("selection counts = %#v; weight 100 must not dominate", counts)
	}
}

func TestRouteRuntimeRandomSeededSequenceIsReproducible(t *testing.T) {
	now := time.Date(2026, 8, 8, 0, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	route := testModelRoute("smart", routeStrategyRandom, 30, "a", "b", "c")
	first := newRouteRuntimeWithRNG(clock, rand.New(rand.NewPCG(11, 13)))
	second := newRouteRuntimeWithRNG(clock, rand.New(rand.NewPCG(11, 13)))
	first.Sync([]modelRoute{route})
	second.Sync([]modelRoute{route})
	for index := range 50 {
		got, want := first.Select(route).model, second.Select(route).model
		if got != want {
			t.Fatalf("draw %d: %q != %q", index, got, want)
		}
	}
}

func TestRouteRuntimeLastKnownGoodPrefersLastSuccess(t *testing.T) {
	now := time.Date(2026, 8, 8, 0, 0, 0, 0, time.UTC)
	runtime := newRouteRuntime(func() time.Time { return now })
	route := testModelRoute("smart", routeStrategyLastKnownGood, 30, "a", "b")
	runtime.Sync([]modelRoute{route})
	if selected := runtime.Select(route); selected.model != "a" {
		t.Fatalf("selection before any success = %#v, want a", selected)
	}
	runtime.MarkSuccess(route, "b")
	for index := range 3 {
		if selected := runtime.Select(route); selected.model != "b" {
			t.Fatalf("selection %d after success = %#v, want b", index, selected)
		}
	}
}

func TestRouteRuntimeLastKnownGoodFallsBackToPriority(t *testing.T) {
	now := time.Date(2026, 8, 8, 0, 0, 0, 0, time.UTC)
	runtime := newRouteRuntime(func() time.Time { return now })
	route := testModelRoute("smart", routeStrategyLastKnownGood, 30, "a", "b", "c")
	route.LastKnownGood = lastKnownGoodPolicy{SuccessRateThreshold: 0.5, MinimumSamples: 1, ObservationWindowMinutes: 60}
	runtime.Sync([]modelRoute{route})
	runtime.MarkSuccess(route, "b")
	runtime.MarkFailure(route, "b")
	if selected := runtime.Select(route); selected.model != "a" {
		t.Fatalf("selection with cooling last success = %#v, want a", selected)
	}
	excluded := map[string]struct{}{routeKey("b"): {}}
	if selected := runtime.SelectExcluding(route, excluded); selected.model != "a" {
		t.Fatalf("selection with excluded last success = %#v, want a", selected)
	}
	now = now.Add(31 * time.Second)
	if selected := runtime.Select(route); selected.model != "b" {
		t.Fatalf("selection after cooldown = %#v, want restored last success b", selected)
	}
}

func TestRouteRuntimeCloneKeepsLastKnownGoodOnlyForSameSignature(t *testing.T) {
	route := testModelRoute("smart", routeStrategyLastKnownGood, 30, "a", "b")
	route.LastKnownGood = lastKnownGoodPolicy{SuccessRateThreshold: 0.5, MinimumSamples: 1, ObservationWindowMinutes: 60}
	runtime := newRouteRuntime(nil)
	runtime.Sync([]modelRoute{route})
	runtime.MarkSuccess(route, "b")

	unchanged := runtime.Clone([]modelRoute{route})
	if selected := unchanged.Select(route); selected.model != "b" {
		t.Fatalf("unchanged clone selection = %#v, want b", selected)
	}
	changed := route
	changed.Targets = []modelTarget{{Model: "a", Weight: defaultTargetWeight}, {Model: "c", Weight: defaultTargetWeight}}
	reset := runtime.Clone([]modelRoute{changed})
	if selected := reset.Select(changed); selected.model != "a" {
		t.Fatalf("changed clone selection = %#v, want a", selected)
	}
}

func TestRouteRuntimeClonePreservesObservations(t *testing.T) {
	now := time.Date(2026, 8, 8, 0, 0, 0, 0, time.UTC)
	route := testModelRoute("lkg", routeStrategyLastKnownGood, 30, "a", "b")
	route.LastKnownGood.MinimumSamples = 1
	route.LastKnownGood.SuccessRateThreshold = 1
	runtime := newRouteRuntime(func() time.Time { return now })
	runtime.Sync([]modelRoute{route})
	runtime.MarkSuccess(route, "b")
	clone := runtime.Clone([]modelRoute{route})
	if selected := clone.Select(route); selected.model != "b" {
		t.Fatalf("clone selection = %#v, want b", selected)
	}
}

func TestRouteRuntimeLastKnownGoodChoosesHigherRate(t *testing.T) {
	now := time.Date(2026, 8, 8, 0, 0, 0, 0, time.UTC)
	route := testModelRoute("lkg", routeStrategyLastKnownGood, 30, "early", "late")
	route.LastKnownGood.MinimumSamples = 2
	route.LastKnownGood.SuccessRateThreshold = 0.5
	runtime := newRouteRuntime(func() time.Time { return now })
	runtime.Sync([]modelRoute{route})
	runtime.MarkSuccess(route, "early")
	runtime.MarkFailure(route, "early")
	runtime.MarkSuccess(route, "late")
	runtime.MarkSuccess(route, "late")
	if selected := runtime.Select(route); selected.model != "late" {
		t.Fatalf("selection = %#v, want late", selected)
	}
}

func TestRouteRuntimeLastKnownGoodQualifiesAtMinimumSamplesAndThreshold(t *testing.T) {
	now := time.Date(2026, 8, 8, 0, 0, 0, 0, time.UTC)
	route := testModelRoute("lkg", routeStrategyLastKnownGood, 30, "a", "b")
	route.LastKnownGood = lastKnownGoodPolicy{SuccessRateThreshold: 0.5, MinimumSamples: 2, ObservationWindowMinutes: 60}
	runtime := newRouteRuntime(func() time.Time { return now })
	runtime.Sync([]modelRoute{route})
	runtime.MarkSuccess(route, "b")
	if selected := runtime.Select(route); selected.model != "a" {
		t.Fatalf("selection with one sample = %#v, want priority a", selected)
	}
	runtime.MarkFailure(route, "b")
	now = now.Add(31 * time.Second)
	if selected := runtime.Select(route); selected.model != "b" {
		t.Fatalf("selection at MinimumSamples with rate == threshold = %#v, want b", selected)
	}
}

func TestRouteRuntimeLastKnownGoodPrunesStaleObservations(t *testing.T) {
	now := time.Date(2026, 8, 8, 0, 0, 0, 0, time.UTC)
	route := testModelRoute("lkg", routeStrategyLastKnownGood, 30, "a", "b")
	route.LastKnownGood = lastKnownGoodPolicy{SuccessRateThreshold: 0.5, MinimumSamples: 1, ObservationWindowMinutes: 60}
	runtime := newRouteRuntime(func() time.Time { return now })
	runtime.Sync([]modelRoute{route})
	runtime.MarkSuccess(route, "b")
	if selected := runtime.Select(route); selected.model != "b" {
		t.Fatalf("selection with fresh success = %#v, want b", selected)
	}
	now = now.Add(61 * time.Minute)
	if selected := runtime.Select(route); selected.model != "a" {
		t.Fatalf("selection after observation window = %#v, want priority a", selected)
	}
}

func TestRouteRuntimeRandomCloneConcurrentSelect(t *testing.T) {
	route := testModelRoute("smart", routeStrategyRandom, 30, "a", "b", "c")
	base := newRouteRuntimeWithRNG(nil, rand.New(rand.NewPCG(7, 11)))
	base.Sync([]modelRoute{route})
	valid := map[string]bool{"a": true, "b": true, "c": true}
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 50 {
				clone := base.Clone([]modelRoute{route})
				if clone.rng == base.rng {
					t.Error("clone shares RNG with base runtime")
				}
				if selected := clone.Select(route); !valid[selected.model] {
					t.Errorf("clone selection = %#v", selected)
				}
				if selected := base.Select(route); !valid[selected.model] {
					t.Errorf("base selection = %#v", selected)
				}
			}
		}()
	}
	wg.Wait()
}

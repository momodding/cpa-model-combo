package main

import (
	cryptorand "crypto/rand"
	"encoding/binary"
	"math/rand/v2"
	"strings"
	"sync"
	"time"
)

type routeRuntime struct {
	mu     sync.Mutex
	now    func() time.Time
	rng    *rand.Rand
	states map[string]*routeState
}

type routeObservation struct {
	at      time.Time
	success bool
}

type routeState struct {
	signature        string
	cursor           int64
	cooldowns        map[string]time.Time
	accountCooldowns map[string]map[string]time.Time
	lastSuccess      string
	observations     map[string][]routeObservation
	lastHeadroom     headroomSelection
	headroomSkips    []headroomSelection
}

type routeSelection struct {
	model      string
	allCooling bool
	retryAfter time.Duration
}

type routingSkip struct {
	Model      string
	Credential accountCredential
	RetryAt    time.Time
}

func newRouteRuntime(now func() time.Time) *routeRuntime {
	if now == nil {
		now = time.Now
	}
	seed1, seed2 := runtimeSeeds(now)
	return newRouteRuntimeWithRNG(now, rand.New(rand.NewPCG(seed1, seed2)))
}

func runtimeSeeds(now func() time.Time) (uint64, uint64) {
	var bytes [16]byte
	if _, err := cryptorand.Read(bytes[:]); err == nil {
		return binary.LittleEndian.Uint64(bytes[:8]), binary.LittleEndian.Uint64(bytes[8:])
	}
	seed := uint64(now().UnixNano())
	return seed, seed ^ 0x9e3779b97f4a7c15
}

func newRouteRuntimeWithRNG(now func() time.Time, rng *rand.Rand) *routeRuntime {
	if now == nil {
		now = time.Now
	}
	if rng == nil {
		rng = rand.New(rand.NewPCG(1, 2))
	}
	return &routeRuntime{now: now, rng: rng, states: make(map[string]*routeState)}
}

func (runtime *routeRuntime) Sync(routes []modelRoute) {
	if runtime == nil {
		return
	}
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	next := make(map[string]*routeState, len(routes))
	for _, route := range routes {
		key := routeKey(route.Alias)
		signature := routeSignature(route)
		state := runtime.states[key]
		if state == nil || state.signature != signature {
			state = &routeState{signature: signature, cooldowns: make(map[string]time.Time), observations: make(map[string][]routeObservation)}
		}
		next[key] = state
	}
	runtime.states = next
}

func (runtime *routeRuntime) Clone(routes []modelRoute) *routeRuntime {
	if runtime == nil {
		cloned := newRouteRuntime(nil)
		cloned.Sync(routes)
		return cloned
	}
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	cloned := newRouteRuntimeWithRNG(runtime.now, rand.New(rand.NewPCG(runtime.rng.Uint64(), runtime.rng.Uint64())))
	for _, route := range routes {
		key := routeKey(route.Alias)
		signature := routeSignature(route)
		old := runtime.states[key]
		state := &routeState{signature: signature, cooldowns: make(map[string]time.Time), observations: make(map[string][]routeObservation)}
		if old != nil && old.signature == signature {
			state.cursor = old.cursor
			state.lastSuccess = old.lastSuccess
			for model, observations := range old.observations {
				state.observations[model] = append([]routeObservation(nil), observations...)
			}
			for model, until := range old.cooldowns {
				state.cooldowns[model] = until
			}
			for model, accounts := range old.accountCooldowns {
				if state.accountCooldowns == nil {
					state.accountCooldowns = make(map[string]map[string]time.Time)
				}
				state.accountCooldowns[model] = make(map[string]time.Time, len(accounts))
				for id, until := range accounts {
					state.accountCooldowns[model][id] = until
				}
			}
		}
		cloned.states[key] = state
	}
	return cloned
}

func (runtime *routeRuntime) Select(route modelRoute) routeSelection {
	return runtime.SelectExcluding(route, nil)
}

func (runtime *routeRuntime) SelectExcluding(route modelRoute, excluded map[string]struct{}) routeSelection {
	if runtime == nil || len(route.Targets) == 0 {
		return routeSelection{}
	}
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	state := runtime.stateLocked(route)
	if state == nil {
		return routeSelection{}
	}
	now := runtime.now()
	var eligible []string
	var earliest time.Time
	if route.Strategy == routeStrategyRandom || route.Strategy == routeStrategyLastKnownGood {
		for _, target := range route.Targets {
			model := strings.TrimSpace(target.Model)
			if _, skip := excluded[routeKey(model)]; skip {
				continue
			}
			until := state.cooldowns[routeKey(model)]
			if until.IsZero() || !now.Before(until) {
				eligible = append(eligible, model)
			} else if earliest.IsZero() || until.Before(earliest) {
				earliest = until
			}
		}
		if len(eligible) > 0 {
			if route.Strategy == routeStrategyRandom {
				return routeSelection{model: eligible[runtime.rng.IntN(len(eligible))]}
			}
			bestModel := ""
			bestRate := float64(-1)
			for _, model := range eligible {
				rate, qualified := lastKnownGoodRate(route, state, model, now)
				if qualified && (bestModel == "" || rate > bestRate) {
					bestModel, bestRate = model, rate
				}
			}
			if bestModel != "" {
				return routeSelection{model: bestModel}
			}
			return routeSelection{model: eligible[0]}
		}
	} else if route.Strategy != routeStrategyRoundRobin {
		for _, target := range route.Targets {
			model := strings.TrimSpace(target.Model)
			if _, skip := excluded[routeKey(model)]; skip {
				continue
			}
			until := state.cooldowns[routeKey(model)]
			if until.IsZero() || !now.Before(until) {
				delete(state.cooldowns, routeKey(model))
				return routeSelection{model: model}
			}
			if earliest.IsZero() || until.Before(earliest) {
				earliest = until
			}
		}
	} else {
		totalWeight := routeTargetWeightTotal(route.Targets)
		if totalWeight <= 0 {
			return routeSelection{}
		}
		start := state.cursor % totalWeight
		if start < 0 {
			start += totalWeight
		}
		startIndex, startOffset := weightedTargetAt(route.Targets, start)
		slot := start
		for offset := 0; offset < len(route.Targets); offset++ {
			index := (startIndex + offset) % len(route.Targets)
			target := route.Targets[index]
			model := strings.TrimSpace(target.Model)
			if _, skip := excluded[routeKey(model)]; !skip {
				until := state.cooldowns[routeKey(model)]
				if until.IsZero() || !now.Before(until) {
					delete(state.cooldowns, routeKey(model))
					state.cursor = (slot + 1) % totalWeight
					return routeSelection{model: model}
				}
				if earliest.IsZero() || until.Before(earliest) {
					earliest = until
				}
			}
			weight := int64(target.Weight)
			if offset == 0 {
				weight -= startOffset
			}
			if weight > 0 {
				slot = (slot + weight) % totalWeight
			}
		}
	}
	if earliest.IsZero() {
		return routeSelection{}
	}
	return routeSelection{allCooling: true, retryAfter: earliest.Sub(now)}
}

func (runtime *routeRuntime) Preflight(route modelRoute, accounts map[string][]accountCredential, unavailable map[string]struct{}) (map[string][]accountCredential, map[string]struct{}, []routingSkip, time.Duration, bool) {
	eligible := make(map[string][]accountCredential, len(route.Targets))
	excluded := make(map[string]struct{}, len(unavailable))
	var skips []routingSkip
	var earliest time.Time
	anyEligible, anyUnavailable := false, false
	if runtime == nil {
		return accounts, unavailable, nil, 0, false
	}
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	state := runtime.stateLocked(route)
	if state == nil {
		return accounts, unavailable, nil, 0, false
	}
	now := runtime.now()
	for _, target := range route.Targets {
		model := strings.TrimSpace(target.Model)
		key := routeKey(model)
		if _, missing := unavailable[key]; missing {
			excluded[key] = struct{}{}
			anyUnavailable = true
			continue
		}
		if until := state.cooldowns[key]; now.Before(until) {
			excluded[key] = struct{}{}
			skips = append(skips, routingSkip{Model: model, RetryAt: until})
			if earliest.IsZero() || until.Before(earliest) {
				earliest = until
			}
			continue
		}
		if len(target.AllowedAccounts) == 0 && route.Strategy != routeStrategyHeadroom {
			eligible[key] = accounts[key]
			anyEligible = true
			continue
		}
		var cooling []routingSkip
		for _, account := range accounts[key] {
			until := state.accountCooldowns[key][account.ID]
			if account.NextRetryAfter.After(until) {
				until = account.NextRetryAfter
			}
			if now.Before(until) {
				cooling = append(cooling, routingSkip{Model: model, Credential: account, RetryAt: until})
			} else {
				eligible[key] = append(eligible[key], account)
			}
		}
		if len(eligible[key]) != 0 {
			anyEligible = true
			skips = append(skips, cooling...)
			continue
		}
		excluded[key] = struct{}{}
		if len(cooling) == 0 {
			anyUnavailable = true
			continue
		}
		first := cooling[0]
		for _, skip := range cooling[1:] {
			if skip.RetryAt.Before(first.RetryAt) {
				first.RetryAt = skip.RetryAt
			}
		}
		if len(cooling) != 1 {
			first.Credential = accountCredential{}
		}
		skips = append(skips, first)
		if earliest.IsZero() || first.RetryAt.Before(earliest) {
			earliest = first.RetryAt
		}
	}
	if !anyEligible && !anyUnavailable && !earliest.IsZero() {
		return eligible, excluded, skips, earliest.Sub(now), true
	}
	return eligible, excluded, skips, 0, false
}

func cooldownFor(route modelRoute) time.Duration {
	if route.CooldownSeconds > 0 {
		return time.Duration(route.CooldownSeconds) * time.Second
	}
	return defaultCooldownSeconds * time.Second
}

func (runtime *routeRuntime) MarkFailure(route modelRoute, model string) {
	if runtime == nil || strings.TrimSpace(model) == "" {
		return
	}
	cooldown := cooldownFor(route)
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	state := runtime.stateLocked(route)
	if state != nil {
		state.cooldowns[routeKey(model)] = runtime.now().Add(cooldown)
		now := runtime.now()
		model = strings.TrimSpace(model)
		state.observations[model] = append(pruneObservations(state.observations[model], now, route.LastKnownGood.ObservationWindowMinutes), routeObservation{at: now})
	}
}

func (runtime *routeRuntime) MarkCooldown(route modelRoute, model string, until time.Time) {
	if runtime == nil || strings.TrimSpace(model) == "" {
		return
	}
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	if state := runtime.stateLocked(route); state != nil && until.After(state.cooldowns[routeKey(model)]) {
		state.cooldowns[routeKey(model)] = until
	}
}

func (runtime *routeRuntime) MarkAccountCooldown(route modelRoute, model, id string, until time.Time) {
	if runtime == nil || strings.TrimSpace(model) == "" || id == "" {
		return
	}
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	state := runtime.stateLocked(route)
	if state == nil {
		return
	}
	if state.accountCooldowns == nil {
		state.accountCooldowns = make(map[string]map[string]time.Time)
	}
	key := routeKey(model)
	if state.accountCooldowns[key] == nil {
		state.accountCooldowns[key] = make(map[string]time.Time)
	}
	if until.After(state.accountCooldowns[key][id]) {
		state.accountCooldowns[key][id] = until
	}
}

func (runtime *routeRuntime) MarkAccountFailure(route modelRoute, model, id string) {
	if runtime == nil || strings.TrimSpace(model) == "" || id == "" {
		return
	}
	cooldown := cooldownFor(route)
	runtime.MarkAccountCooldown(route, model, id, runtime.now().Add(cooldown))
	runtime.RecordFailure(route, model)
}

func (runtime *routeRuntime) RecordFailure(route modelRoute, model string) {
	if runtime == nil || strings.TrimSpace(model) == "" {
		return
	}
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	if state := runtime.stateLocked(route); state != nil {
		now := runtime.now()
		model = strings.TrimSpace(model)
		state.observations[model] = append(pruneObservations(state.observations[model], now, route.LastKnownGood.ObservationWindowMinutes), routeObservation{at: now})
	}
}

func (runtime *routeRuntime) MarkAccountSuccess(route modelRoute, model, id string) {
	if runtime == nil {
		return
	}
	runtime.mu.Lock()
	if state := runtime.stateLocked(route); state != nil {
		delete(state.accountCooldowns[routeKey(model)], id)
		model = strings.TrimSpace(model)
		state.lastSuccess = model
		now := runtime.now()
		state.observations[model] = append(pruneObservations(state.observations[model], now, route.LastKnownGood.ObservationWindowMinutes), routeObservation{at: now, success: true})
	}
	runtime.mu.Unlock()
}
func (runtime *routeRuntime) stateLocked(route modelRoute) *routeState {
	key := routeKey(route.Alias)
	if key == "" {
		return nil
	}
	if runtime.states == nil {
		runtime.states = make(map[string]*routeState)
	}
	signature := routeSignature(route)
	state := runtime.states[key]
	if state == nil || state.signature != signature {
		state = &routeState{signature: signature, cooldowns: make(map[string]time.Time), observations: make(map[string][]routeObservation)}
		runtime.states[key] = state
	}
	return state
}

func routeTargetWeightTotal(targets []modelTarget) int64 {
	var total int64
	for _, target := range targets {
		weight := int64(target.Weight)
		if weight < 1 || total > (1<<63-1)-weight {
			return 0
		}
		total += weight
	}
	return total
}

func weightedTargetAt(targets []modelTarget, slot int64) (int, int64) {
	var offset int64
	for index, target := range targets {
		weight := int64(target.Weight)
		if weight < 1 {
			continue
		}
		if slot < offset+weight {
			return index, slot - offset
		}
		offset += weight
	}
	return 0, 0
}

func lastKnownGoodRate(route modelRoute, state *routeState, model string, now time.Time) (float64, bool) {
	obs := pruneObservations(state.observations[model], now, route.LastKnownGood.ObservationWindowMinutes)
	state.observations[model] = obs
	if len(obs) < route.LastKnownGood.MinimumSamples {
		return 0, false
	}
	successes := 0
	for _, item := range obs {
		if item.success {
			successes++
		}
	}
	rate := float64(successes) / float64(len(obs))
	return rate, rate >= route.LastKnownGood.SuccessRateThreshold
}

func qualifiesLastKnownGood(route modelRoute, state *routeState, model string, now time.Time) bool {
	_, qualified := lastKnownGoodRate(route, state, model, now)
	return qualified
}

func pruneObservations(obs []routeObservation, now time.Time, minutes int) []routeObservation {
	cutoff := now.Add(-time.Duration(minutes) * time.Minute)
	first := 0
	for first < len(obs) && obs[first].at.Before(cutoff) {
		first++
	}
	return obs[first:]
}

func (runtime *routeRuntime) MarkSuccess(route modelRoute, model string) {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	state := runtime.stateLocked(route)
	if state != nil {
		if state.observations == nil {
			state.observations = make(map[string][]routeObservation)
		}
		model = strings.TrimSpace(model)
		delete(state.cooldowns, routeKey(model))
		state.lastSuccess = model
		now := runtime.now()
		state.observations[model] = append(pruneObservations(state.observations[model], now, route.LastKnownGood.ObservationWindowMinutes), routeObservation{at: now, success: true})
	}
}

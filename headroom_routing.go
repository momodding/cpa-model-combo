package main

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
)

type headroomSelection struct {
	Model      string            `json:"model"`
	Credential accountCredential `json:"credential"`
	Score      float64           `json:"score"`
	State      string            `json:"state"`
	Reason     string            `json:"reason,omitempty"`
}

type headroomCandidate struct {
	target     string
	credential accountCredential
	score      float64
	state      quotaState
}

func rankHeadroom(route modelRoute, accounts map[string][]accountCredential, snapshots map[string]quotaSnapshot, requestedModel string, unknownScore float64) ([]headroomCandidate, []headroomSelection) {
	var candidates []headroomCandidate
	var skips []headroomSelection
	for _, target := range route.Targets {
		for _, credential := range accounts[routeKey(target.Model)] {
			score, state := headroom(snapshots[quotaKey(credential)], targetModel(requestedModel, target.Model))
			reason := ""
			if state == quotaExhausted {
				reason = "quota_exhausted"
			}
			if state == quotaUnknown && route.UnknownQuota == "exclude" {
				reason = "quota_unknown"
			}
			if reason != "" {
				skips = append(skips, headroomSelection{Model: target.Model, Credential: credential, Score: score, State: state.String(), Reason: reason})
				continue
			}
			if state == quotaUnknown {
				score = unknownScore
			}
			candidates = append(candidates, headroomCandidate{target: target.Model, credential: credential, score: score, state: state})
		}
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		a, b := candidates[i], candidates[j]
		if route.UnknownQuota == "last" && (a.state == quotaUnknown) != (b.state == quotaUnknown) {
			return a.state != quotaUnknown
		}
		if a.score != b.score {
			return a.score > b.score
		}
		if (a.state == quotaUnknown) != (b.state == quotaUnknown) {
			return a.state != quotaUnknown
		}
		return a.state != quotaUnlimited && b.state == quotaUnlimited
	})
	return candidates, skips
}

type affinityHost interface {
	AffinityLookup(pluginapi.HostAffinityLookupRequest) (pluginapi.HostAffinityLookupResponse, error)
}

func (p *modelRouterPlugin) headroomCandidates(ctx context.Context, request pluginapi.ExecutorRequest, host modelHost, route modelRoute, accounts map[string][]accountCredential) []headroomCandidate {
	var credentials []accountCredential
	for _, target := range route.Targets {
		credentials = append(credentials, accounts[routeKey(target.Model)]...)
	}
	var snapshots map[string]quotaSnapshot
	if p.quota != nil {
		snapshots = p.quota.Get(ctx, credentials)
	}
	candidates, skips := rankHeadroom(route, accounts, snapshots, request.Model, p.config.QuotaPolling.DefaultUnknownQuotaPercent/100)
	p.runtime.mu.Lock()
	if state := p.runtime.stateLocked(route); state != nil {
		state.headroomSkips = skips
		state.lastHeadroom = headroomSelection{}
	}
	p.runtime.mu.Unlock()
	session := ""
	for _, key := range []string{"canonical_session_id", "session_id", "execution_session_id", "derived_session_id", "lcp_affinity_session_id"} {
		if value, ok := request.Metadata[key].(string); ok && strings.TrimSpace(value) != "" {
			session = strings.TrimSpace(value)
			break
		}
	}
	if source, ok := host.(affinityHost); ok && session != "" {
		type lookupKey struct{ provider, model string }
		lookups := make(map[lookupKey]pluginapi.HostAffinityLookupResponse)
		for i, candidate := range candidates {
			key := lookupKey{candidate.credential.Provider, targetModel(request.Model, candidate.target)}
			binding, found := lookups[key]
			if !found {
				binding, _ = source.AffinityLookup(pluginapi.HostAffinityLookupRequest{Provider: key.provider, Model: key.model, SessionID: session})
				lookups[key] = binding
			}
			if binding.Status == pluginapi.HostAffinityStatusBound && !binding.Disabled && !binding.Unavailable && binding.AuthIndex != "" && binding.AuthIndex == candidate.credential.AuthIndex {
				copy(candidates[1:i+1], candidates[:i])
				candidates[0] = candidate
				break
			}
		}
	}
	return candidates
}

func (p *modelRouterPlugin) recordHeadroom(route modelRoute, candidate headroomCandidate, reason string) {
	p.runtime.mu.Lock()
	if state := p.runtime.stateLocked(route); state != nil {
		state.lastHeadroom = headroomSelection{Model: candidate.target, Credential: candidate.credential, Score: candidate.score, State: candidate.state.String(), Reason: reason}
	}
	p.runtime.mu.Unlock()
}

func (p *modelRouterPlugin) executeHeadroom(request pluginapi.ExecutorRequest, host modelHost, route modelRoute, accounts map[string][]accountCredential, identity correlationIdentity, bodyInfo executionBody, routerRequestID string, policy fallbackPolicy) (pluginapi.ExecutorResponse, error) {
	candidates := p.headroomCandidates(context.Background(), request, host, route, accounts)
	limit := p.config.attemptLimit(len(candidates))
	var lastErr error
	lastTarget, reason := "", ""
	for attempt, candidate := range candidates[:limit] {
		target := targetModel(request.Model, candidate.target)
		p.recordHeadroom(route, candidate, reason)
		ordinal := p.nextCorrelationAttempt(identity)
		if ordinal < 0 {
			ordinal = attempt
		}
		p.attribution.MarkRouted(route.Alias, target, request.Headers, routerRequestID, ordinal)
		response, err := host.Execute(hostRequest(request, bodyInfo, target, false, candidate.credential))
		status := response.StatusCode
		if status == 0 && err == nil {
			status = http.StatusOK
		}
		if status == 0 && err != nil {
			status = statusFromError(err)
		}
		if err == nil && status >= 200 && status < 300 {
			p.runtime.MarkAccountSuccess(route, candidate.target, candidate.credential.ID)
			metadata := map[string]any{"route_alias": route.Alias, "selected_model": target, "selected_index": attempt, "headroom_score": candidate.score, "quota_state": candidate.state.String()}
			if lastTarget != "" {
				metadata["fallback_attempt"] = attempt
				metadata["fallback_from"] = lastTarget
				metadata["fallback_reason"] = reason
			}
			return pluginapi.ExecutorResponse{Payload: rewriteResponseModel(response.Body, request.Model), Headers: cloneHeader(response.Headers), Metadata: metadata}, nil
		}
		if err == nil {
			err = statusError{status: status, message: hostStatusMessage(target, status, response.Body)}
		}
		if terminalRequestError(err) {
			return pluginapi.ExecutorResponse{}, safeRestrictedFailure(err)
		}
		retry := eligibleRouteFailure(err, policy)
		reason = fallbackReason(status, nil)
		err = safeRestrictedFailure(err)
		if !retry || p.config.FallbackDisabled {
			p.recordHeadroom(route, candidate, reason)
			return pluginapi.ExecutorResponse{}, err
		}
		lastErr, lastTarget = err, target
		p.recordHeadroom(route, candidate, reason)
		p.runtime.MarkAccountFailure(route, candidate.target, candidate.credential.ID)
	}
	detail := ""
	if lastErr != nil {
		detail = lastErr.Error()
	}
	return pluginapi.ExecutorResponse{}, newRouteError(http.StatusServiceUnavailable, "model_route_unavailable", route.Alias, fmt.Sprintf("no available candidate for model route %q", route.Alias), detail)
}

func (p *modelRouterPlugin) streamHeadroom(ctx context.Context, request pluginapi.ExecutorRequest, streamID string, host modelHost, onPayload func(), route modelRoute, accounts map[string][]accountCredential, identity correlationIdentity, bodyInfo executionBody, routerRequestID string, policy fallbackPolicy) error {
	candidates := p.headroomCandidates(ctx, request, host, route, accounts)
	limit := p.config.attemptLimit(len(candidates))
	var lastErr error
	reason := ""
	for attempt, candidate := range candidates[:limit] {
		if err := ctx.Err(); err != nil {
			return err
		}
		target := targetModel(request.Model, candidate.target)
		p.recordHeadroom(route, candidate, reason)
		ordinal := p.nextCorrelationAttempt(identity)
		if ordinal < 0 {
			ordinal = attempt
		}
		p.attribution.MarkRouted(route.Alias, target, request.Headers, routerRequestID, ordinal)
		received, err := forwardStreamAttempt(ctx, request, bodyInfo, target, request.Model, streamID, host, onPayload, candidate.credential)
		if err == nil {
			p.runtime.MarkAccountSuccess(route, candidate.target, candidate.credential.ID)
			return nil
		}
		if cancelled := ctx.Err(); cancelled != nil {
			return cancelled
		}
		retry := eligibleRouteFailure(err, policy)
		reason = fallbackReason(statusFromError(err), nil)
		err = safeRestrictedFailure(err)
		p.recordHeadroom(route, candidate, reason)
		if retry {
			if received {
				p.runtime.RecordFailure(route, candidate.target)
			} else {
				p.runtime.MarkAccountFailure(route, candidate.target, candidate.credential.ID)
			}
		}
		if received || !retry || p.config.FallbackDisabled {
			return err
		}
		lastErr = err
	}
	detail := ""
	if lastErr != nil {
		detail = lastErr.Error()
	}
	return newRouteError(http.StatusServiceUnavailable, "model_route_unavailable", route.Alias, fmt.Sprintf("no available candidate for model route %q", route.Alias), detail)
}

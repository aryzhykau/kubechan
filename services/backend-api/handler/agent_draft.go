package handler

import (
	"encoding/json"
	"errors"
)

type agentExposure struct {
	NodeIDs      []string `json:"nodeIds"`
	EdgeIDs      []string `json:"edgeIds"`
	CandidateIDs []string `json:"candidateIds"`
	SourceIDs    []string `json:"sourceIds"`
}

type agentDraft struct {
	Version       int             `json:"version"`
	Revision      int             `json:"revision"`
	HasConclusion bool            `json:"hasConclusion"`
	Assessment    json.RawMessage `json:"assessment"`
	Graph         agentExposure   `json:"graph"`
	Branches      []agentBranch   `json:"branches,omitempty"`
}

type agentBranch struct {
	ID        string                   `json:"id"`
	NodeID    string                   `json:"nodeId"`
	EdgeID    string                   `json:"edgeId,omitempty"`
	FindingID string                   `json:"findingId,omitempty"`
	Question  string                   `json:"question"`
	Status    string                   `json:"status"`
	Priority  int                      `json:"priority,omitempty"`
	Outcome   string                   `json:"outcome"`
	SourceIDs []string                 `json:"sourceIds"`
	Runbook   []agentBranchRunbookStep `json:"runbook,omitempty"`
}

type agentBranchRunbookStep struct {
	Action       string `json:"action"`
	Verification string `json:"verification"`
}

type agentDraftChange struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

type agentDraftValidator struct {
	nodes, edges, candidates, sources map[string]bool
	endpoints                         map[string][]string
	brokenRefs                        map[string][]string
	seen                              agentExposure
	shown                             agentExposure
	entry                             string
	revision                          int
	branches                          map[string]bool
}

func newAgentDraftValidator(payload map[string]any) *agentDraftValidator {
	v := &agentDraftValidator{nodes: map[string]bool{}, edges: map[string]bool{}, candidates: map[string]bool{}, sources: map[string]bool{}, endpoints: map[string][]string{}, brokenRefs: map[string][]string{}}
	var graph struct {
		EntryResourceID string
		Nodes           []struct {
			ID, Type, State string
			SourceIDs       []string
		}
		Edges []struct {
			ID, From, To, Origin, State string
			SourceIDs                   []string
		}
		Candidates []struct{ ID, SourceNodeID, ReferenceNodeID, CandidateNodeID string }
	}
	body, _ := json.Marshal(payload["graph"])
	_ = json.Unmarshal(body, &graph)
	v.entry = graph.EntryResourceID
	missing := map[string][]string{}
	for _, n := range graph.Nodes {
		v.nodes[n.ID] = true
		if n.Type == "reference" && n.State == "missing" {
			missing[n.ID] = n.SourceIDs
		}
	}
	for _, e := range graph.Edges {
		v.edges[e.ID] = true
		v.endpoints[e.ID] = []string{e.From, e.To}
		if sources, ok := missing[e.To]; ok && e.Origin == "observed" && e.State == "broken" {
			v.brokenRefs[e.ID] = append(e.SourceIDs, sources...)
		}
	}
	for _, c := range graph.Candidates {
		v.candidates[c.ID] = true
		v.endpoints[c.ID] = []string{c.SourceNodeID, c.ReferenceNodeID, c.CandidateNodeID}
	}
	var sources []struct {
		ID, Path string
		Resource struct{ Kind string }
	}
	body, _ = json.Marshal(payload["sources"])
	_ = json.Unmarshal(body, &sources)
	for _, s := range sources {
		if s.Resource.Kind != "Secret" || s.Path == "/exists" {
			v.sources[s.ID] = true
		}
	}
	return v
}

func agentIDSet(ids []string) map[string]bool {
	result := make(map[string]bool, len(ids))
	for _, id := range ids {
		result[id] = true
	}
	return result
}

func validAgentIDs(ids []string, allowed map[string]bool) bool {
	if len(ids) > 2048 {
		return false
	}
	seen := map[string]bool{}
	for _, id := range ids {
		if len(id) > 512 || !allowed[id] || seen[id] {
			return false
		}
		seen[id] = true
	}
	return true
}

func (v *agentDraftValidator) validate(event agentProgress) error {
	if t := event.TurnTokens; t != nil && (event.Stage != "context_delivered" || event.Turn < 1 || t.Input < 0 || t.Output < 0 || t.Input > 1_000_000_000 || t.Output > 1_000_000_000) {
		return errors.New("invalid model turn token usage")
	}
	if scope := event.InvestigationGraph; scope != nil {
		read := event.Stage == "get_resource" || event.Stage == "get_events" || event.Stage == "get_logs" || event.Stage == "read_sources"
		initial := event.Stage == "indexing" && len(v.shown.NodeIDs) == 0
		drawing := event.Stage == "draft_updated" && event.Change != nil && event.Change.Kind == "graph"
		startingRead := read && event.Phase == "started" && event.Target != nil
		nodes := agentIDSet(scope.NodeIDs)
		if (!startingRead && (event.Phase != "completed" || (!initial && !read && !drawing))) || !nodes[v.entry] ||
			!validAgentIDs(scope.NodeIDs, v.nodes) || !validAgentIDs(scope.EdgeIDs, v.edges) ||
			!validAgentIDs(scope.CandidateIDs, v.candidates) || !validAgentIDs(scope.SourceIDs, v.sources) {
			return errors.New("invalid live investigation graph")
		}
		if initial && (len(nodes) != 1 || len(scope.SourceIDs) != 0 || len(scope.EdgeIDs) != 0 || len(scope.CandidateIDs) != 0) {
			return errors.New("initial investigation graph must contain only the root")
		}
		for _, ids := range [][2][]string{{v.shown.NodeIDs, scope.NodeIDs}, {v.shown.EdgeIDs, scope.EdgeIDs}, {v.shown.CandidateIDs, scope.CandidateIDs}, {v.shown.SourceIDs, scope.SourceIDs}} {
			if !validAgentIDs(ids[0], agentIDSet(ids[1])) {
				return errors.New("live investigation graph regressed")
			}
		}
		for _, id := range append(append([]string{}, scope.EdgeIDs...), scope.CandidateIDs...) {
			for _, endpoint := range v.endpoints[id] {
				if !nodes[endpoint] {
					return errors.New("live investigation relationship has hidden endpoints")
				}
			}
		}
		if startingRead && (!nodes[event.Target.NodeID] || len(scope.EdgeIDs) != len(v.shown.EdgeIDs) ||
			len(scope.CandidateIDs) != len(v.shown.CandidateIDs) || len(scope.SourceIDs) != len(v.shown.SourceIDs) ||
			!validAgentIDs(scope.NodeIDs, agentIDSet(append(append([]string{}, v.shown.NodeIDs...), event.Target.NodeID)))) {
			return errors.New("read start may only reveal its canonical target")
		}
		if drawing {
			if len(scope.SourceIDs) != len(v.shown.SourceIDs) ||
				!validAgentIDs(scope.NodeIDs, agentIDSet(v.seen.NodeIDs)) ||
				!validAgentIDs(scope.EdgeIDs, agentIDSet(v.seen.EdgeIDs)) ||
				!validAgentIDs(scope.CandidateIDs, agentIDSet(v.seen.CandidateIDs)) {
				return errors.New("drawing references undelivered snapshot context")
			}
			allowed := agentIDSet(v.shown.NodeIDs)
			sources := agentIDSet(v.seen.SourceIDs)
			previousEdges := agentIDSet(v.shown.EdgeIDs)
			for _, id := range scope.EdgeIDs {
				proof, broken := v.brokenRefs[id]
				if !broken || previousEdges[id] {
					continue
				}
				confirmed := false
				for _, source := range proof {
					confirmed = confirmed || sources[source]
				}
				if !confirmed {
					return errors.New("drawing a missing reference requires delivered evidence")
				}
				allowed[v.endpoints[id][1]] = true
			}
			if !validAgentIDs(scope.NodeIDs, allowed) {
				return errors.New("drawing may only add linked missing references")
			}
		}
		v.shown = *scope
	}
	if event.Exposed != nil {
		if event.Stage != "context_delivered" || !validAgentIDs(event.Exposed.NodeIDs, v.nodes) || !validAgentIDs(event.Exposed.EdgeIDs, v.edges) || !validAgentIDs(event.Exposed.CandidateIDs, v.candidates) || !validAgentIDs(event.Exposed.SourceIDs, v.sources) {
			return errors.New("invalid delivered snapshot references")
		}
		for _, pair := range [][2][]string{{v.seen.NodeIDs, event.Exposed.NodeIDs}, {v.seen.EdgeIDs, event.Exposed.EdgeIDs}, {v.seen.CandidateIDs, event.Exposed.CandidateIDs}, {v.seen.SourceIDs, event.Exposed.SourceIDs}} {
			if !validAgentIDs(pair[0], agentIDSet(pair[1])) {
				return errors.New("delivered context regressed")
			}
		}
		v.seen = *event.Exposed
	}
	if event.Change != nil {
		allowed := map[string]bool{"claim": true, "hypothesis": true, "gap": true, "remediation": true, "review": true, "graph": true, "conclusion": true, "path": true, "retracted": true, "branch": true}
		if event.Stage != "draft_updated" || !allowed[event.Change.Kind] || event.Change.ID == "" || len(event.Change.ID) > 512 {
			return errors.New("invalid draft change")
		}
	}
	if event.BranchID != "" && !v.branches[event.BranchID] {
		return errors.New("unknown investigation branch")
	}
	if event.Draft == nil {
		if event.Stage == "draft_updated" || event.Stage == "context_delivered" && event.Exposed == nil {
			return errors.New("missing live investigation payload")
		}
		return nil
	}
	d := event.Draft
	initial := event.Stage == "indexing" && event.Phase == "completed" && d.Revision == 0 && v.revision == 0
	if d.Version != 1 || (!initial && (event.Stage != "draft_updated" || d.Revision != v.revision+1 || d.Revision > 50 || event.Change == nil)) {
		return errors.New("invalid draft revision")
	}
	body, _ := json.Marshal(d)
	if len(body) > 48_000 || !validAgentIDs(d.Graph.NodeIDs, agentIDSet(v.seen.NodeIDs)) || !validAgentIDs(d.Graph.EdgeIDs, agentIDSet(v.seen.EdgeIDs)) || !validAgentIDs(d.Graph.CandidateIDs, agentIDSet(v.seen.CandidateIDs)) || len(d.Graph.SourceIDs) != 0 {
		return errors.New("invalid draft graph scope")
	}
	nodes := agentIDSet(d.Graph.NodeIDs)
	if len(d.Branches) > 8 {
		return errors.New("too many investigation branches")
	}
	branchIDs, branchEdges := map[string]bool{}, map[string]bool{}
	deliveredEdges := agentIDSet(v.seen.EdgeIDs)
	deliveredNodes, deliveredSources := agentIDSet(v.seen.NodeIDs), agentIDSet(v.seen.SourceIDs)
	for _, branch := range d.Branches {
		switch branch.Status {
		case "open", "checking", "confirmed", "ruled_out", "gap":
		default:
			return errors.New("invalid investigation branch status")
		}
		if branch.ID == "" || len(branch.ID) > 512 || branchIDs[branch.ID] || !deliveredNodes[branch.NodeID] ||
			branch.Question == "" || len([]rune(branch.Question)) > 250 || len([]rune(branch.Outcome)) > 350 ||
			(branch.Priority < 0 || branch.Priority > 8) ||
			(branch.Status != "open" && branch.Status != "checking" && branch.Outcome == "") || len(branch.SourceIDs) > 6 ||
			((branch.Status == "confirmed" || branch.Status == "ruled_out") && len(branch.SourceIDs) == 0) ||
			!validAgentIDs(branch.SourceIDs, deliveredSources) {
			return errors.New("invalid investigation branch")
		}
		if branch.EdgeID != "" {
			endpoints := v.endpoints[branch.EdgeID]
			if !deliveredEdges[branch.EdgeID] || branchEdges[branch.EdgeID] ||
				len(endpoints) != 2 || endpoints[0] != branch.NodeID {
				return errors.New("invalid investigation branch relationship")
			}
			branchEdges[branch.EdgeID] = true
		}
		if len(branch.Runbook) > 3 || (len(branch.Runbook) > 0 && branch.Status != "confirmed") {
			return errors.New("invalid investigation branch runbook")
		}
		for _, step := range branch.Runbook {
			if step.Action == "" || len([]rune(step.Action)) > 250 || step.Verification == "" || len([]rune(step.Verification)) > 200 {
				return errors.New("invalid investigation branch runbook step")
			}
		}
		branchIDs[branch.ID] = true
	}
	if event.Change != nil && event.Change.Kind == "branch" && !branchIDs[event.Change.ID] {
		return errors.New("invalid investigation branch change")
	}
	if event.Change != nil && event.Change.Kind == "graph" && (event.InvestigationGraph == nil || !validAgentIDs(d.Graph.NodeIDs, agentIDSet(v.shown.NodeIDs))) {
		return errors.New("model cards must belong to the live drawing")
	}
	for _, id := range append(append([]string{}, d.Graph.EdgeIDs...), d.Graph.CandidateIDs...) {
		for _, nodeID := range v.endpoints[id] {
			if !nodes[nodeID] {
				return errors.New("draft relationship has unreviewed endpoints")
			}
		}
	}
	var assessment struct {
		Version                       int
		Summary                       string
		Confidence                    float64
		Claims, AlternativeHypotheses []struct {
			ID, Statement, Verdict string
			Citations              []struct{ SourceID, Quote string }
		}
		Remediation []struct {
			Citations []struct{ SourceID, Quote string }
		}
		NodeInsights []struct {
			NodeID, Relevance, Comment string
			SourceIDs, ClaimIDs        []string
		}
		CausalPath *struct {
			EntryResourceID, RootCauseEntityID string
			Steps                              []struct{ EntityID, EdgeID, CandidateID, ClaimID string }
		}
	}
	if json.Unmarshal(d.Assessment, &assessment) != nil || assessment.Version != 2 || assessment.Confidence < 0 || assessment.Confidence > 1 || len(assessment.Summary) > 8000 || d.HasConclusion && assessment.Summary == "" {
		return errors.New("invalid draft assessment")
	}
	sources := agentIDSet(v.seen.SourceIDs)
	claims, supportedClaims := map[string]bool{}, map[string]bool{}
	for _, c := range assessment.Claims {
		if c.ID == "" || claims[c.ID] {
			return errors.New("invalid draft claim ID")
		}
		claims[c.ID] = true
		supportedClaims[c.ID] = c.Verdict == "supported"
	}
	for _, branch := range d.Branches {
		if branch.FindingID != "" && (branch.Status != "confirmed" || !supportedClaims[branch.FindingID]) {
			return errors.New("invalid investigation branch finding")
		}
	}
	for _, c := range append(assessment.Claims, assessment.AlternativeHypotheses...) {
		if (c.Verdict != "supported" && c.Verdict != "partially_supported" && c.Verdict != "unsupported") || c.Verdict != "unsupported" && len(c.Citations) == 0 {
			return errors.New("invalid draft claim")
		}
		for _, cite := range c.Citations {
			if !sources[cite.SourceID] || cite.Quote != "" {
				return errors.New("draft cites unread evidence or model quote")
			}
		}
	}
	for _, s := range assessment.Remediation {
		for _, c := range s.Citations {
			if !sources[c.SourceID] || c.Quote != "" {
				return errors.New("invalid draft remediation citation")
			}
		}
	}
	reviews := map[string]bool{}
	for _, n := range assessment.NodeInsights {
		if !nodes[n.NodeID] || reviews[n.NodeID] || n.Comment == "" || len(n.Comment) > 3200 || (n.Relevance != "relevant" && n.Relevance != "not_relevant" && n.Relevance != "context") || !validAgentIDs(n.SourceIDs, sources) || !validAgentIDs(n.ClaimIDs, claims) {
			return errors.New("invalid draft resource review")
		}
		reviews[n.NodeID] = true
	}
	if len(reviews) != len(nodes) {
		return errors.New("draft graph must match explicit resource reviews")
	}
	if p := assessment.CausalPath; p != nil {
		seenNodes, seenEdges, seenCandidates := agentIDSet(v.seen.NodeIDs), agentIDSet(v.seen.EdgeIDs), agentIDSet(v.seen.CandidateIDs)
		if !seenNodes[p.EntryResourceID] || !seenNodes[p.RootCauseEntityID] {
			return errors.New("invalid draft causal scope")
		}
		for _, s := range p.Steps {
			if !seenNodes[s.EntityID] || s.EdgeID != "" && !seenEdges[s.EdgeID] || s.CandidateID != "" && !seenCandidates[s.CandidateID] || s.ClaimID != "" && !claims[s.ClaimID] {
				return errors.New("invalid draft causal reference")
			}
		}
	}
	v.branches = branchIDs
	v.revision = d.Revision
	return nil
}

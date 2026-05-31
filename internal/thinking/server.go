package thinking

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"
)

// SequentialThinkingServer holds the per-session state for one client of the
// criticalthinking tool. Construct exactly one per session: in HTTP mode this
// happens inside the StreamableHTTP factory closure; in stdio mode there is
// one global instance for the process.
//
// The factory-closure pattern is the cross-session isolation invariant. There
// is intentionally no map keyed by session-id anywhere — the closure scope is
// the only addressable path to a session's state.
type SequentialThinkingServer struct {
	mu             sync.Mutex
	thoughtHistory []ThoughtData
	branches       map[string][]ThoughtData
	confidenceSum  float64
	confidenceN    int
	branchConfSum  map[string]float64
	branchConfN    map[string]int
	lastAccessed   time.Time
}

// NewServer returns an empty SequentialThinkingServer.
func NewServer() *SequentialThinkingServer {
	return &SequentialThinkingServer{
		branches:      make(map[string][]ThoughtData),
		branchConfSum: make(map[string]float64),
		branchConfN:   make(map[string]int),
		lastAccessed:  time.Now(),
	}
}

// HistoryLength returns the number of thoughts in the trunk + branches
// (a single append-only log).
func (s *SequentialThinkingServer) HistoryLength() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.thoughtHistory)
}

// SessionConfidence returns the running mean confidence over trunk thoughts.
// Returns 0 when no trunk thoughts have been recorded.
func (s *SequentialThinkingServer) SessionConfidence() float64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.confidenceN == 0 {
		return 0
	}
	return s.confidenceSum / float64(s.confidenceN)
}

// LastAccessed returns the time of the last successful ProcessThought call.
// Used by the HTTP idle-timeout cleanup goroutine in main.go.
func (s *SequentialThinkingServer) LastAccessed() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastAccessed
}

// ToolResult is the package-internal return type from ProcessThought. main.go
// adapts it into a *mcp.CallToolResult — keeping mcp imports out of this
// package preserves its testability.
type ToolResult struct {
	Text           string // the thinking-out-loud transcript (or error JSON when IsError)
	StructuredJSON string // JSON-encoded ThoughtResponse, "" when IsError
	IsError        bool
}

// ProcessThought validates input, mutates state, and returns either a
// transcript+structured response or an error result. The Go-level error
// return is reserved for unrecoverable internal faults (currently never
// returned); validation failures produce IsError=true results.
func (s *SequentialThinkingServer) ProcessThought(td ThoughtData) (ToolResult, error) {
	if err := td.Validate(); err != nil {
		return errorResult(err), nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Cross-field validation against state.
	if td.RevisesThought != nil && *td.RevisesThought > len(s.thoughtHistory) {
		return errorResult(fmt.Errorf("revisesThought %d out of range (history length %d)",
			*td.RevisesThought, len(s.thoughtHistory))), nil
	}
	if td.BranchFromThought != nil && *td.BranchFromThought > len(s.thoughtHistory) {
		return errorResult(fmt.Errorf("branchFromThought %d out of range (history length %d)",
			*td.BranchFromThought, len(s.thoughtHistory))), nil
	}

	if td.ThoughtNumber > td.TotalThoughts {
		td.TotalThoughts = td.ThoughtNumber
	}

	s.thoughtHistory = append(s.thoughtHistory, td)
	s.lastAccessed = time.Now()

	if td.BranchFromThought != nil && td.BranchID != "" {
		s.branches[td.BranchID] = append(s.branches[td.BranchID], td)
	}

	onBranch := td.BranchFromThought != nil && td.BranchID != ""
	if onBranch {
		s.branchConfSum[td.BranchID] += td.Confidence
		s.branchConfN[td.BranchID]++
	} else {
		s.confidenceSum += td.Confidence
		s.confidenceN++
	}

	var branchConf map[string]float64
	if len(s.branchConfN) > 0 {
		branchConf = make(map[string]float64, len(s.branchConfN))
		for k, n := range s.branchConfN {
			branchConf[k] = s.branchConfSum[k] / float64(n)
		}
	}

	sessionConf := 0.0
	if s.confidenceN > 0 {
		sessionConf = s.confidenceSum / float64(s.confidenceN)
	}

	resp := ThoughtResponse{
		ThoughtNumber:        td.ThoughtNumber,
		TotalThoughts:        td.TotalThoughts,
		NextThoughtNeeded:    *td.NextThoughtNeeded,
		Branches:             sortedKeys(s.branches),
		ThoughtHistoryLength: len(s.thoughtHistory),
		SessionConfidence:    sessionConf,
		BranchConfidences:    branchConf,
	}

	structured, err := json.Marshal(resp)
	if err != nil {
		// Should be impossible for fixed-shape struct.
		return errorResult(fmt.Errorf("marshal response: %w", err)), nil
	}

	return ToolResult{
		Text:           s.renderTranscriptLocked(td, sessionConf),
		StructuredJSON: string(structured),
		IsError:        false,
	}, nil
}

// renderTranscriptLocked builds the narrated transcript text for one thought.
// Caller must hold s.mu.
func (s *SequentialThinkingServer) renderTranscriptLocked(td ThoughtData, sessionConf float64) string {
	var b strings.Builder

	header := s.headerLineLocked(td)
	fmt.Fprintf(&b, "%s\n\n", header)
	fmt.Fprintln(&b, td.Thought)
	fmt.Fprintln(&b)

	if len(td.Assumptions) == 0 {
		fmt.Fprintln(&b, "  Assumptions: (none claimed)")
	} else {
		fmt.Fprintln(&b, "  Assumptions:")
		for _, a := range td.Assumptions {
			fmt.Fprintf(&b, "    - %s\n", a)
		}
	}
	fmt.Fprintln(&b)

	fmt.Fprintln(&b, "  Critique:")
	fmt.Fprintf(&b, "    %s\n\n", td.Critique)

	fmt.Fprintln(&b, "  Counter-argument:")
	fmt.Fprintf(&b, "    %s\n\n", td.CounterArgument)

	if *td.NextThoughtNeeded {
		fmt.Fprintf(&b, "  Next, I want to: %s\n\n", td.NextStepRationale)
	}

	s.renderFooterLocked(&b, td, sessionConf)
	return b.String()
}

// headerLineLocked picks one of four header forms based on revision/branch state.
// Caller must hold s.mu.
func (s *SequentialThinkingServer) headerLineLocked(td ThoughtData) string {
	switch {
	case td.IsRevision != nil && *td.IsRevision && td.RevisesThought != nil:
		return fmt.Sprintf("Revision of thought %d (now thought %d) · confidence %.2f",
			*td.RevisesThought, td.ThoughtNumber, td.Confidence)
	case td.BranchFromThought != nil && td.BranchID != "":
		// First-in-branch vs subsequent: count the current branch's depth.
		// At this point the new thought has already been appended to s.branches[BranchID].
		depth := len(s.branches[td.BranchID])
		if depth <= 1 {
			return fmt.Sprintf("Branch '%s' from thought %d · confidence %.2f",
				td.BranchID, *td.BranchFromThought, td.Confidence)
		}
		return fmt.Sprintf("Branch '%s' · thought %d · confidence %.2f",
			td.BranchID, td.ThoughtNumber, td.Confidence)
	default:
		return fmt.Sprintf("Thought %d of %d · confidence %.2f",
			td.ThoughtNumber, td.TotalThoughts, td.Confidence)
	}
}

// renderFooterLocked writes either the trunk or branch+trunk footer.
// Caller must hold s.mu.
func (s *SequentialThinkingServer) renderFooterLocked(b *strings.Builder, td ThoughtData, sessionConf float64) {
	onBranch := td.BranchFromThought != nil && td.BranchID != ""
	if onBranch {
		bn := s.branchConfN[td.BranchID]
		bc := 0.0
		if bn > 0 {
			bc = s.branchConfSum[td.BranchID] / float64(bn)
		}
		bnoun := "thought"
		if bn != 1 {
			bnoun = "thoughts"
		}
		fmt.Fprintf(b, "— branch '%s' confidence %.2f across %d %s\n",
			td.BranchID, bc, bn, bnoun)
		tnoun := "thought"
		if s.confidenceN != 1 {
			tnoun = "thoughts"
		}
		fmt.Fprintf(b, "— session confidence (trunk) %.2f across %d %s",
			sessionConf, s.confidenceN, tnoun)
		return
	}
	noun := "thought"
	if s.confidenceN != 1 {
		noun = "thoughts"
	}
	fmt.Fprintf(b, "— session confidence %.2f across %d %s",
		sessionConf, s.confidenceN, noun)
}

// errorResult formats a validation/runtime error in the JS-compatible
// {error, status: "failed"} shape.
func errorResult(err error) ToolResult {
	// Marshaling a fixed-shape {string,string} struct cannot fail.
	body, _ := json.Marshal(struct {
		Error  string `json:"error"`
		Status string `json:"status"`
	}{Error: err.Error(), Status: "failed"})
	return ToolResult{Text: string(body), IsError: true}
}

func sortedKeys(m map[string][]ThoughtData) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

// HistorySnapshot returns a deep copy of the trunk + branch thought history,
// safe to marshal and ship to a resource consumer. Branches are keyed by id.
type HistorySnapshot struct {
	Thoughts []ThoughtData            `json:"thoughts"`
	Branches map[string][]ThoughtData `json:"branches,omitempty"`
}

// Snapshot returns the current state for the thinking://current resource.
// The returned slices and map are safe to mutate without affecting the server.
func (s *SequentialThinkingServer) Snapshot() HistorySnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()

	thoughts := make([]ThoughtData, len(s.thoughtHistory))
	copy(thoughts, s.thoughtHistory)

	var branches map[string][]ThoughtData
	if len(s.branches) > 0 {
		branches = make(map[string][]ThoughtData, len(s.branches))
		for k, v := range s.branches {
			cp := make([]ThoughtData, len(v))
			copy(cp, v)
			branches[k] = cp
		}
	}
	return HistorySnapshot{Thoughts: thoughts, Branches: branches}
}

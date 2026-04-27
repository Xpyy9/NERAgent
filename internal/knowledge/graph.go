package knowledge

import (
	"sync"
	"time"
)

// AnalysisDepth tracks how thoroughly a class has been analyzed.
type AnalysisDepth int

const (
	Discovered     AnalysisDepth = iota // name known from search/xref
	StructureKnown                      // getClassWithStructure done
	CodeAnalyzed                        // code read and analyzed
	DeepAnalyzed                        // callers/data flow traced
)

func (d AnalysisDepth) String() string {
	switch d {
	case Discovered:
		return "discovered"
	case StructureKnown:
		return "structure_known"
	case CodeAnalyzed:
		return "code_analyzed"
	case DeepAnalyzed:
		return "deep_analyzed"
	default:
		return "unknown"
	}
}

// ClassEntity represents a Java class discovered during analysis.
type ClassEntity struct {
	Name        string
	SuperClass  string
	Interfaces  []string
	Depth       AnalysisDepth
	Methods     []string // method keys (className#methodName)
	Fields      []string
	FindingRefs []int // indices into Graph.Findings
	IsExported  bool
	SourceTool  string // which tool discovered it
}

// MethodEntity represents a Java method.
type MethodEntity struct {
	ClassName      string
	MethodName     string
	Signature      string
	Callers        []string // caller method keys (class#method)
	IsTainted      bool     // is a taint source/sink
	IsSink         bool     // matches a known security sink API
	SinkCategory   string   // sink category: crypto/webview/exec/sql/file/intent/network
	SinkPattern    string   // human-readable pattern that matched (e.g. "doFinal", "loadUrl")
	IsSource       bool     // matches a known external input source API
	SourceCategory string   // source category: intent/deeplink/network/file/clipboard
	SourcePattern  string   // human-readable pattern that matched (e.g. "getStringExtra", "getData")
}

// ComponentEntity represents an Android component from the manifest.
type ComponentEntity struct {
	Name          string
	Type          string // activity/service/receiver/provider
	Exported      bool
	IntentFilters []string
	Permission    string
}

// Finding represents a confirmed security finding.
type Finding struct {
	ID          int
	Type        string // vulnerability/crypto/component/info
	Severity    string // critical/high/medium/low/info
	Description string
	Evidence    []CodeEvidence
	ClassRef    string // class where found
	MethodRef   string // method where found
}

// CodeEvidence links a finding to a specific tool call and code.
type CodeEvidence struct {
	ToolCall string // e.g. "getClassWithStructure(com.example.Foo)"
	Snippet  string // relevant code lines
}

// DataFlowChain represents a taint data flow path.
type DataFlowChain struct {
	Source    TaintNode
	Sink      TaintNode
	Waypoints []TaintNode
	Risk      string
}

// TaintNode represents a point in a data flow.
type TaintNode struct {
	Class  string
	Method string
	Type   string // source type: intent/deeplink/network/file/clipboard
}

// ChatTurn records one round of user question + LLM conclusion summary.
type ChatTurn struct {
	UserQuery string
	Summary   string
	Timestamp time.Time
}

// CallRecord tracks repeated invocations of the same tool action+params combination.
// Used by InterceptedTool to enforce the "no more than 2 identical calls" rule programmatically.
type CallRecord struct {
	Count      int
	LastResult string
}

// Graph is the central knowledge store for an APK analysis session.
type Graph struct {
	mu sync.RWMutex

	Classes    map[string]*ClassEntity     // key: fully qualified name
	Methods    map[string]*MethodEntity    // key: className#methodName
	Components map[string]*ComponentEntity // key: component name

	Findings  []*Finding
	DataFlows []*DataFlowChain

	ChatHistory []ChatTurn
	CallHistory map[string]*CallRecord // key: "action:param1:param2"
	PackageName string
	CreatedAt   time.Time
	LastActive  time.Time
}

// NewGraph creates an empty knowledge graph.
func NewGraph() *Graph {
	return &Graph{
		Classes:     make(map[string]*ClassEntity),
		Methods:     make(map[string]*MethodEntity),
		Components:  make(map[string]*ComponentEntity),
		CallHistory: make(map[string]*CallRecord),
		CreatedAt:   time.Now(),
		LastActive:  time.Now(),
	}
}

// Touch updates the last active timestamp.
func (g *Graph) Touch() {
	g.mu.Lock()
	g.LastActive = time.Now()
	g.mu.Unlock()
}

// AddChatTurn appends a conversation turn, keeping at most 10 recent turns.
func (g *Graph) AddChatTurn(userQuery, summary string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.ChatHistory = append(g.ChatHistory, ChatTurn{
		UserQuery: userQuery,
		Summary:   summary,
		Timestamp: time.Now(),
	})
	if len(g.ChatHistory) > 10 {
		g.ChatHistory = g.ChatHistory[len(g.ChatHistory)-10:]
	}
}

// EnsureClass returns the ClassEntity for the given name, creating it at Discovered depth if absent.
func (g *Graph) EnsureClass(name string) *ClassEntity {
	g.mu.Lock()
	defer g.mu.Unlock()
	if c, ok := g.Classes[name]; ok {
		return c
	}
	c := &ClassEntity{Name: name, Depth: Discovered}
	g.Classes[name] = c
	return c
}

// EnsureMethod returns the MethodEntity for className#methodName, creating if absent.
func (g *Graph) EnsureMethod(className, methodName string) *MethodEntity {
	key := className + "#" + methodName
	g.mu.Lock()
	defer g.mu.Unlock()
	if m, ok := g.Methods[key]; ok {
		return m
	}
	m := &MethodEntity{ClassName: className, MethodName: methodName}
	g.Methods[key] = m
	return m
}

// AddFinding appends a finding if no duplicate description exists. Returns the finding ID.
func (g *Graph) AddFinding(f *Finding) int {
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, existing := range g.Findings {
		if existing.Description == f.Description {
			return existing.ID
		}
	}
	f.ID = len(g.Findings)
	g.Findings = append(g.Findings, f)
	return f.ID
}

// AddComponent registers a manifest component.
func (g *Graph) AddComponent(comp *ComponentEntity) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.Components[comp.Name] = comp
}

// AddDataFlow appends a data flow chain if no duplicate source->sink exists.
func (g *Graph) AddDataFlow(df *DataFlowChain) {
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, existing := range g.DataFlows {
		if existing.Source.Class == df.Source.Class && existing.Source.Method == df.Source.Method &&
			existing.Sink.Class == df.Sink.Class && existing.Sink.Method == df.Sink.Method {
			return
		}
	}
	g.DataFlows = append(g.DataFlows, df)
}

// Stats returns analysis coverage statistics (read-locked).
func (g *Graph) Stats() (total, discovered, structureKnown, codeAnalyzed, deepAnalyzed int) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	for _, c := range g.Classes {
		total++
		switch c.Depth {
		case Discovered:
			discovered++
		case StructureKnown:
			structureKnown++
		case CodeAnalyzed:
			codeAnalyzed++
		case DeepAnalyzed:
			deepAnalyzed++
		}
	}
	return
}

// ExportedComponents returns all exported component names (read-locked).
func (g *Graph) ExportedComponents() []string {
	g.mu.RLock()
	defer g.mu.RUnlock()
	var out []string
	for _, comp := range g.Components {
		if comp.Exported {
			out = append(out, comp.Name)
		}
	}
	return out
}

// UnanalyzedExported returns exported components whose classes have not been deep-analyzed.
func (g *Graph) UnanalyzedExported() []string {
	g.mu.RLock()
	defer g.mu.RUnlock()
	var out []string
	for _, comp := range g.Components {
		if !comp.Exported {
			continue
		}
		cls, ok := g.Classes[comp.Name]
		if !ok || cls.Depth < CodeAnalyzed {
			out = append(out, comp.Name)
		}
	}
	return out
}

// FindingsBySeverity returns count per severity level.
func (g *Graph) FindingsBySeverity() map[string]int {
	g.mu.RLock()
	defer g.mu.RUnlock()
	counts := make(map[string]int)
	for _, f := range g.Findings {
		counts[f.Severity]++
	}
	return counts
}

// Snapshot captures objective progress metrics at a point in time.
// Used to detect stagnation between replanner rounds.
type Snapshot struct {
	ClassesTotal      int
	CodeAnalyzedCount int                // classes at CodeAnalyzed or deeper
	DeepAnalyzed      int
	MethodsTotal      int                // total methods tracked in graph
	FindingsTotal     int
	HighSeverity      int                // critical + high count
	SinksHit          int
	SinkCategoriesHit map[string]int     // category -> hit count
	SourcesHit          int
	SourceCategoriesHit map[string]int   // category -> hit count
	DataFlowsTotal    int
}

// TakeSnapshot returns the current objective progress snapshot.
func (g *Graph) TakeSnapshot() *Snapshot {
	g.mu.RLock()
	defer g.mu.RUnlock()

	snap := &Snapshot{
		SinkCategoriesHit:   make(map[string]int),
		SourceCategoriesHit: make(map[string]int),
	}
	for _, c := range g.Classes {
		snap.ClassesTotal++
		if c.Depth >= CodeAnalyzed {
			snap.CodeAnalyzedCount++
		}
		if c.Depth >= DeepAnalyzed {
			snap.DeepAnalyzed++
		}
	}
	snap.MethodsTotal = len(g.Methods)
	snap.FindingsTotal = len(g.Findings)
	for _, f := range g.Findings {
		if f.Severity == "critical" || f.Severity == "high" {
			snap.HighSeverity++
		}
	}
	for _, m := range g.Methods {
		if m.IsSink {
			snap.SinksHit++
			if m.SinkCategory != "" {
				snap.SinkCategoriesHit[m.SinkCategory]++
			}
		}
		if m.IsSource {
			snap.SourcesHit++
			if m.SourceCategory != "" {
				snap.SourceCategoriesHit[m.SourceCategory]++
			}
		}
	}
	snap.DataFlowsTotal = len(g.DataFlows)
	return snap
}

// Equals returns true if two snapshots reflect identical progress (no new evidence accumulated).
func (s *Snapshot) Equals(other *Snapshot) bool {
	if s == nil || other == nil {
		return false
	}
	if s.ClassesTotal != other.ClassesTotal ||
		s.CodeAnalyzedCount != other.CodeAnalyzedCount ||
		s.DeepAnalyzed != other.DeepAnalyzed ||
		s.MethodsTotal != other.MethodsTotal ||
		s.FindingsTotal != other.FindingsTotal ||
		s.HighSeverity != other.HighSeverity ||
		s.SinksHit != other.SinksHit ||
		s.SourcesHit != other.SourcesHit ||
		s.DataFlowsTotal != other.DataFlowsTotal {
		return false
	}
	if len(s.SinkCategoriesHit) != len(other.SinkCategoriesHit) {
		return false
	}
	for k, v := range s.SinkCategoriesHit {
		if other.SinkCategoriesHit[k] != v {
			return false
		}
	}
	if len(s.SourceCategoriesHit) != len(other.SourceCategoriesHit) {
		return false
	}
	for k, v := range s.SourceCategoriesHit {
		if other.SourceCategoriesHit[k] != v {
			return false
		}
	}
	return true
}

// HasNewEvidence returns true if other has more findings, sinks, classes, or analyzed code than s.
// Used to distinguish "no progress" from "made progress but no new findings".
func (s *Snapshot) HasNewEvidence(prev *Snapshot) bool {
	if prev == nil {
		return s.FindingsTotal > 0 || s.SinksHit > 0 || s.SourcesHit > 0 ||
			s.DeepAnalyzed > 0 || s.ClassesTotal > 0 || s.MethodsTotal > 0
	}
	return s.FindingsTotal > prev.FindingsTotal ||
		s.SinksHit > prev.SinksHit ||
		s.SourcesHit > prev.SourcesHit ||
		s.DeepAnalyzed > prev.DeepAnalyzed ||
		s.CodeAnalyzedCount > prev.CodeAnalyzedCount ||
		s.ClassesTotal > prev.ClassesTotal ||
		s.MethodsTotal > prev.MethodsTotal ||
		s.DataFlowsTotal > prev.DataFlowsTotal ||
		s.HighSeverity > prev.HighSeverity
}

// RecordCall logs a tool invocation and returns the updated count for this call key.
func (g *Graph) RecordCall(key, result string) int {
	g.mu.Lock()
	defer g.mu.Unlock()
	rec, ok := g.CallHistory[key]
	if !ok {
		rec = &CallRecord{}
		g.CallHistory[key] = rec
	}
	rec.Count++
	rec.LastResult = result
	return rec.Count
}

// GetCallRecord returns the call record for a key (nil if never called).
func (g *Graph) GetCallRecord(key string) *CallRecord {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.CallHistory[key]
}

package graph

import (
	"encoding/json"
	"path"
	"regexp"
	"strconv"
	"strings"
)

// specExtractor parses the four spec artifacts. It relies on the explicit
// traceability annotations the csdd templates already demand
// (internal/templater/templates/spec/*.tmpl), so ~95% of the edges it emits are
// EXTRACTED with confidence 1.0.
type specExtractor struct{}

func (specExtractor) Matches(p string) bool {
	if specNameFromPath(p) == "" {
		return false
	}
	base := path.Base(p)
	switch base {
	case "spec.json", "requirements.md", "design.md", "tasks.md":
		return true
	}
	return false
}

func (specExtractor) Extract(src Source) ([]Fragment, error) {
	spec := specNameFromPath(src.Path)
	if spec == "" {
		return nil, nil
	}
	switch path.Base(src.Path) {
	case "spec.json":
		return extractSpecJSON(spec, src)
	case "requirements.md":
		return extractRequirements(spec, src), nil
	case "design.md":
		return extractDesign(spec, src), nil
	case "tasks.md":
		return extractTasks(spec, src), nil
	}
	return nil, nil
}

// specNodeID / criterionID / taskID / designID / interfaceID / flowID are the
// canonical ID constructors. Because "." normalizes to "_", a whole annotation
// token ("1.2") and its split parts ("1","2") both produce the same ID, so
// references resolve regardless of how they were written.
func specNodeID(spec string) string        { return MakeID("spec", spec) }
func requirementID(spec, n string) string  { return MakeID("req", spec, n) }
func criterionID(spec, ref string) string  { return MakeID("crit", spec, ref) }
func taskID(spec, ref string) string       { return MakeID("task", spec, ref) }
func designID(spec, comp string) string    { return MakeID("design", spec, comp) }
func interfaceID(spec, name string) string { return MakeID("iface", spec, name) }
func flowID(spec, name string) string      { return MakeID("flow", spec, name) }

func extractSpecJSON(spec string, src Source) ([]Fragment, error) {
	var doc struct {
		FeatureName     string          `json:"feature_name"`
		Language        string          `json:"language"`
		Phase           string          `json:"phase"`
		DevelopmentFlow string          `json:"development_flow"`
		Ready           bool            `json:"ready_for_implementation"`
		Approvals       json.RawMessage `json:"approvals"`
	}
	// A malformed spec.json still yields a bare spec node so the folder is indexed.
	_ = json.Unmarshal(src.Content, &doc)
	label := doc.FeatureName
	if label == "" {
		label = spec
	}
	attrs := map[string]any{}
	if doc.Phase != "" {
		attrs["phase"] = doc.Phase
	}
	if doc.DevelopmentFlow != "" {
		attrs["development_flow"] = doc.DevelopmentFlow
	}
	if doc.Language != "" {
		attrs["language"] = doc.Language
	}
	attrs["ready_for_implementation"] = doc.Ready
	return []Fragment{{Nodes: []Node{{
		ID: specNodeID(spec), Label: label, FileType: TypeSpec,
		SourceFile: src.Path, SourceLocation: "L1", Attrs: attrs,
	}}}}, nil
}

var reRequirementHeading = regexp.MustCompile(`^###\s+Requirement\s+(\d+)\s*:?\s*(.*)$`)
var reNumberedItem = regexp.MustCompile(`^(\d+)\.\s+(.*)$`)

func extractRequirements(spec string, src Source) []Fragment {
	lines := normLines(src.Content)
	var frag Fragment
	curReq := ""   // current requirement number
	curReqID := "" // current requirement node ID
	inCriteria := false
	for i, raw := range lines {
		line := strings.TrimRight(raw, " ")
		if m := reRequirementHeading.FindStringSubmatch(strings.TrimSpace(line)); m != nil {
			curReq = m[1]
			title := strings.TrimSpace(m[2])
			curReqID = requirementID(spec, curReq)
			label := "Requirement " + curReq
			if title != "" {
				label += ": " + title
			}
			frag.Nodes = append(frag.Nodes, Node{
				ID: curReqID, Label: label, FileType: TypeRequire,
				SourceFile: src.Path, SourceLocation: loc(i),
			})
			inCriteria = false
			continue
		}
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "**Acceptance Criteria**") {
			inCriteria = true
			continue
		}
		// A new heading ends the criteria region.
		if strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "**Objective") {
			if !strings.HasPrefix(trimmed, "###") {
				inCriteria = false
			}
		}
		if !inCriteria || curReq == "" {
			continue
		}
		if m := reNumberedItem.FindStringSubmatch(trimmed); m != nil {
			critNum := m[1]
			text := collapseSpaces(m[2])
			cid := criterionID(spec, curReq+"_"+critNum)
			frag.Nodes = append(frag.Nodes, Node{
				ID: cid, Label: text, FileType: TypeCriterion,
				SourceFile: src.Path, SourceLocation: loc(i),
			})
			frag.Edges = append(frag.Edges, Edge{
				Source: curReqID, Target: cid, Relation: RelHasCriterion,
				Confidence: Extracted, ConfidenceScore: 1.0, SourceFile: src.Path,
			})
		}
	}
	return []Fragment{frag}
}

// reCriterionRef matches a clean N.M criterion reference for validating a
// traceability requirement cell (a value that is not this is preserved as a
// pending/range reference).
var reCriterionRef = regexp.MustCompile(`^\d+\.\d+$`)

func extractDesign(spec string, src Source) []Fragment {
	lines := normLines(src.Content)
	var frag Fragment
	section := ""
	inTraceTable := false
	// Component parsing state.
	curComp := ""
	curCompID := ""
	depSubsection := "" // Inbound | Outbound | External | ""
	inFileStructure := false
	seenNode := map[string]bool{}
	addNode := func(n Node) {
		if seenNode[n.ID] {
			return
		}
		seenNode[n.ID] = true
		frag.Nodes = append(frag.Nodes, n)
	}

	for i, raw := range lines {
		line := rtrim(raw)
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "## ") {
			section = strings.TrimSpace(strings.TrimPrefix(trimmed, "## "))
			inTraceTable = strings.EqualFold(section, "Requirements Traceability")
			inFileStructure = strings.EqualFold(section, "File Structure Plan")
			curComp, curCompID, depSubsection = "", "", ""
			continue
		}

		// --- Requirements Traceability table ---
		if inTraceTable && strings.HasPrefix(trimmed, "|") {
			cells := splitCells(trimmed)
			if isTableSeparator(cells) {
				continue
			}
			if len(cells) >= 1 && strings.EqualFold(cells[0], "Requirement") {
				continue // header
			}
			if len(cells) < 2 {
				continue
			}
			addTraceRow(spec, src, i, cells, &frag, addNode)
			continue
		}

		// --- Components and Interfaces ---
		if strings.EqualFold(section, "Components and Interfaces") {
			if strings.HasPrefix(trimmed, "### ") {
				curComp = strings.TrimSpace(strings.TrimPrefix(trimmed, "### "))
				curCompID = designID(spec, curComp)
				depSubsection = ""
				addNode(Node{
					ID: curCompID, Label: curComp, FileType: TypeDesign,
					SourceFile: src.Path, SourceLocation: loc(i),
				})
				continue
			}
			if curComp == "" {
				continue
			}
			// **Requirements**: 1.1, 1.2  → refined_by (criterion → design)
			if v, ok := boldField(trimmed, "Requirements"); ok {
				for _, ref := range splitList(v) {
					frag.Edges = append(frag.Edges, Edge{
						Source: criterionID(spec, ref), Target: curCompID, Relation: RelRefinedBy,
						Confidence: Extracted, ConfidenceScore: 1.0, SourceFile: src.Path, Ref: ref,
					})
				}
				continue
			}
			// **Dependencies**: begins a sub-block of Inbound/Outbound/External.
			if _, ok := boldField(trimmed, "Dependencies"); ok {
				depSubsection = "deps"
				continue
			}
			if depSubsection != "" {
				if names, kind, ok := depLine(trimmed); ok {
					for _, name := range names {
						switch kind {
						case "External":
							// External dependency → tech (INFERRED); resolved by name.
							tid := MakeID("tech", name)
							addNode(Node{
								ID: tid, Label: name, FileType: TypeTech,
								SourceFile: src.Path, SourceLocation: loc(i),
								Attrs: map[string]any{"status": "referenced"},
							})
							frag.Edges = append(frag.Edges, Edge{
								Source: curCompID, Target: tid, Relation: RelUsesTech,
								Confidence: Inferred, ConfidenceScore: 0.7, SourceFile: src.Path,
							})
						default: // Inbound / Outbound → component_dep (design → design)
							frag.Edges = append(frag.Edges, Edge{
								Source: curCompID, Target: designID(spec, name), Relation: RelComponentDep,
								Confidence: Inferred, ConfidenceScore: 0.8, SourceFile: src.Path, Ref: name,
							})
						}
					}
					continue
				}
			}
		}

		// --- File Structure Plan: cited paths become code_ref nodes ---
		// The reference source is the spec node (not a design component), so the
		// planning list never masquerades as an unimplemented component.
		if inFileStructure {
			for _, cited := range citedPathsInTreeLine(line) {
				emitCitedPath(src, i, cited, specNodeID(spec), &frag)
			}
		}
		// Any component contract or prose line may cite a real repo path.
		for _, cited := range citedPathsInline(line) {
			from := specNodeID(spec)
			if curCompID != "" && strings.EqualFold(section, "Components and Interfaces") {
				from = curCompID
			}
			emitCitedPath(src, i, cited, from, &frag)
		}
	}
	return []Fragment{frag}
}

// addTraceRow emits traced_to / implements / exercises for one traceability row.
func addTraceRow(spec string, src Source, line int, cells []string, frag *Fragment, addNode func(Node)) {
	reqCell := cells[0]
	var comps, ifaces, flows []string
	if len(cells) > 1 {
		comps = splitList(cells[1])
	}
	if len(cells) > 2 {
		ifaces = splitList(cells[2])
	}
	if len(cells) > 3 {
		flows = splitList(cells[3])
	}
	for _, comp := range comps {
		if comp == "—" || comp == "-" {
			continue
		}
		did := designID(spec, comp)
		addNode(Node{ID: did, Label: comp, FileType: TypeDesign, SourceFile: src.Path, SourceLocation: loc(line)})
		for _, ref := range splitList(reqCell) {
			// A malformed requirement cell (e.g. a range "3.1-3.5") is kept as the
			// edge ref so Assemble reports it, never expanded.
			crit := criterionID(spec, ref)
			frag.Edges = append(frag.Edges, Edge{
				Source: crit, Target: did, Relation: RelTracedTo,
				Confidence: Extracted, ConfidenceScore: 1.0, SourceFile: src.Path, Ref: ref,
			})
		}
		for _, iface := range ifaces {
			if iface == "—" || iface == "-" || iface == "" {
				continue
			}
			iid := interfaceID(spec, iface)
			addNode(Node{ID: iid, Label: iface, FileType: TypeInterface, SourceFile: src.Path, SourceLocation: loc(line)})
			frag.Edges = append(frag.Edges, Edge{
				Source: did, Target: iid, Relation: RelImplements,
				Confidence: Extracted, ConfidenceScore: 1.0, SourceFile: src.Path,
			})
		}
	}
	for _, ref := range splitList(reqCell) {
		crit := criterionID(spec, ref)
		for _, flow := range flows {
			if flow == "—" || flow == "-" || flow == "" {
				continue
			}
			fid := flowID(spec, flow)
			addNode(Node{ID: fid, Label: flow, FileType: TypeFlow, SourceFile: src.Path, SourceLocation: loc(line)})
			frag.Edges = append(frag.Edges, Edge{
				Source: crit, Target: fid, Relation: RelExercises,
				Confidence: Extracted, ConfidenceScore: 1.0, SourceFile: src.Path, Ref: ref,
			})
		}
	}
}

var reTaskLine = regexp.MustCompile(`^-\s*\[([ xX])\]\s+([0-9]+(?:\.[0-9]+)*)\.?\s+(.*)$`)
var reAnnotation = regexp.MustCompile(`^_([A-Za-z]+):\s*(.*?)_$`)

func extractTasks(spec string, src Source) []Fragment {
	lines := normLines(src.Content)
	var frag Fragment
	curTaskID := ""
	for i, raw := range lines {
		line := rtrim(raw)
		trimmed := strings.TrimSpace(line)
		if m := reTaskLine.FindStringSubmatch(trimmed); m != nil {
			done := m[1] == "x" || m[1] == "X"
			num := m[2]
			title := strings.TrimSpace(m[3])
			parallel := strings.Contains(title, "(P)")
			curTaskID = taskID(spec, num)
			status := "pending"
			if done {
				status = "done"
			}
			frag.Nodes = append(frag.Nodes, Node{
				ID: curTaskID, Label: cleanTaskLabel(title), FileType: TypeTask,
				SourceFile: src.Path, SourceLocation: loc(i),
				Attrs: map[string]any{"status": status, "parallel": parallel, "number": num},
			})
			// Inline _Boundary:_ on the task line itself.
			for _, b := range inlineBoundaries(title) {
				frag.Edges = append(frag.Edges, Edge{
					Source: curTaskID, Target: designID(spec, b), Relation: RelInBoundary,
					Confidence: Extracted, ConfidenceScore: 1.0, SourceFile: src.Path, Ref: b,
				})
			}
			// A cited path in the task title (e.g. `internal/foo/bar.go`).
			for _, cited := range citedPathsInline(line) {
				emitCitedPath(src, i, cited, curTaskID, &frag)
			}
			continue
		}
		if curTaskID == "" {
			continue
		}
		// A task line or its notes may cite a real repo path (reuses/references).
		for _, cited := range citedPathsInline(line) {
			emitCitedPath(src, i, cited, curTaskID, &frag)
		}
		// Annotation lines: "- _Requirements: 1.1, 1.2_"
		body := strings.TrimSpace(strings.TrimPrefix(trimmed, "-"))
		if m := reAnnotation.FindStringSubmatch(body); m != nil {
			key := m[1]
			val := strings.TrimSpace(m[2])
			switch key {
			case "Requirements":
				for _, ref := range splitList(val) {
					frag.Edges = append(frag.Edges, Edge{
						Source: curTaskID, Target: criterionID(spec, ref), Relation: RelRealizes,
						Confidence: Extracted, ConfidenceScore: 1.0, SourceFile: src.Path, Ref: ref,
					})
				}
			case "Depends":
				for _, ref := range splitList(val) {
					frag.Edges = append(frag.Edges, Edge{
						Source: curTaskID, Target: taskID(spec, ref), Relation: RelDependsOn,
						Confidence: Extracted, ConfidenceScore: 1.0, SourceFile: src.Path, Ref: ref,
					})
				}
			case "Boundary":
				for _, ref := range splitList(val) {
					frag.Edges = append(frag.Edges, Edge{
						Source: curTaskID, Target: designID(spec, ref), Relation: RelInBoundary,
						Confidence: Extracted, ConfidenceScore: 1.0, SourceFile: src.Path, Ref: ref,
					})
				}
			}
		}
	}
	return []Fragment{frag}
}

var reInlineBoundary = regexp.MustCompile(`_Boundary:\s*([^_]+)_`)
var reInlineDepends = regexp.MustCompile(`_Depends:\s*([^_]+)_`)

// cleanTaskLabel strips the inline annotations (_Boundary:_, _Depends:_) and the
// (P) marker from a task title so the node label reads as the plain capability.
func cleanTaskLabel(title string) string {
	title = reInlineBoundary.ReplaceAllString(title, "")
	title = reInlineDepends.ReplaceAllString(title, "")
	title = strings.ReplaceAll(title, "(P)", "")
	return collapseSpaces(title)
}

func inlineBoundaries(title string) []string {
	var out []string
	for _, m := range reInlineBoundary.FindAllStringSubmatch(title, -1) {
		for _, b := range splitList(m[1]) {
			out = append(out, b)
		}
	}
	return out
}

// boldField parses a "**Key**: value" line, returning the value if the key matches.
func boldField(line, key string) (string, bool) {
	prefix := "**" + key + "**"
	if !strings.HasPrefix(line, prefix) {
		return "", false
	}
	rest := strings.TrimSpace(strings.TrimPrefix(line, prefix))
	rest = strings.TrimPrefix(rest, ":")
	return strings.TrimSpace(rest), true
}

// depLine parses an Inbound/Outbound/External dependency sub-line under
// **Dependencies**, e.g. "- Inbound: API, Gateway". Returns the names, the kind,
// and whether the line was a dependency line.
func depLine(line string) (names []string, kind string, ok bool) {
	l := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "-"))
	for _, k := range []string{"Inbound", "Outbound", "External"} {
		if strings.HasPrefix(l, k) {
			rest := strings.TrimSpace(strings.TrimPrefix(l, k))
			rest = strings.TrimPrefix(rest, ":")
			rest = strings.TrimSpace(rest)
			if rest == "" || rest == "..." || rest == "—" {
				return nil, k, true
			}
			return splitList(rest), k, true
		}
	}
	return nil, "", false
}

// isValidCriterionRef reports whether ref is a clean N.M reference.
func isValidCriterionRef(ref string) bool {
	return reCriterionRef.MatchString(ref)
}

// requirementRefParts splits an N.M reference; returns ok=false for non-numeric.
func requirementRefParts(ref string) (n, m int, ok bool) {
	parts := strings.Split(ref, ".")
	if len(parts) != 2 {
		return 0, 0, false
	}
	a, err1 := strconv.Atoi(parts[0])
	b, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil {
		return 0, 0, false
	}
	return a, b, true
}

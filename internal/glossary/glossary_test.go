package glossary

import (
	"strings"
	"testing"
)

const sampleGlossary = `# Glossary

Free prose outside the section is ignored.

## Language

### Core

**Feat**: One row of a plan's Feats table; becomes exactly one spec.
_Avoid_: feature, story

**Purchase Order**: A buyer's request to procure goods.
_Avoid_: PO

### Money

**Invoice**: A bill issued against a purchase order.
`

func parse(t *testing.T) *Glossary {
	t.Helper()
	g := Parse(sampleGlossary)
	if len(g.Terms) != 3 {
		t.Fatalf("got %d terms, want 3: %+v", len(g.Terms), g.Terms)
	}
	return g
}

func TestParseEntriesAndClusters(t *testing.T) {
	g := parse(t)
	if g.Terms[0].Canonical != "Feat" || g.Terms[0].Cluster != "Core" {
		t.Errorf("term 0 = %+v", g.Terms[0])
	}
	if g.Terms[0].Definition == "" || !strings.Contains(g.Terms[0].Definition, "one spec") {
		t.Errorf("term 0 definition = %q", g.Terms[0].Definition)
	}
	if len(g.Terms[0].Avoid) != 2 || g.Terms[0].Avoid[0] != "feature" {
		t.Errorf("term 0 avoid = %v", g.Terms[0].Avoid)
	}
	if g.Terms[2].Cluster != "Money" {
		t.Errorf("term 2 cluster = %q, want Money", g.Terms[2].Cluster)
	}
}

func TestMatchWholeTokenAndMultiWord(t *testing.T) {
	g := parse(t)
	cases := []struct {
		id       string
		canon    string // expected canonical of the (first) match, "" for none
		viaAlias string // expected alias, "" for canonical/none
	}{
		{"feat-store", "Feat", ""},                      // canonical whole-token
		{"feature-flags", "Feat", "feature"},            // avoided alias
		{"purchase-order-import", "Purchase Order", ""}, // multi-word contiguous run
		{"po-sync", "Purchase Order", "PO"},             // alias
		{"defeature", "", ""},                           // substring must NOT match
		{"clientele", "", ""},                           // the canonical non-match guard
	}
	for _, tc := range cases {
		got := g.Match(tc.id)
		if tc.canon == "" {
			if len(got) != 0 {
				t.Errorf("%q: expected no match, got %+v", tc.id, got)
			}
			continue
		}
		found := false
		for _, m := range got {
			if m.Canonical == tc.canon && m.Alias == tc.viaAlias {
				found = true
			}
		}
		if !found {
			t.Errorf("%q: expected match {canon=%q alias=%q}, got %+v", tc.id, tc.canon, tc.viaAlias, got)
		}
	}
}

func TestWellFormednessFindings(t *testing.T) {
	src := `## Language

**Customer**: A person who buys.
_Avoid_: client

**Customer**: A duplicate canonical.

**Order**:
_Avoid_: customer

**Blank**:
`
	g := Parse(src)
	msgs := []string{}
	for _, i := range g.Issues() {
		msgs = append(msgs, i.Msg)
	}
	joined := strings.Join(msgs, "\n")
	for _, want := range []string{
		"duplicate glossary term 'Customer'",
		"glossary alias 'customer' of term 'Order' collides with a canonical term",
		"glossary term 'Blank' has no definition",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing well-formedness issue %q; got:\n%s", want, joined)
		}
	}
}

func TestNormalizeTerm(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"Purchase Order", "purchaseorder"},
		{"purchase-order", "purchaseorder"},
		{"Feat", "feat"},
	} {
		if got := NormalizeTerm(tc.in); got != tc.want {
			t.Errorf("NormalizeTerm(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestAbsentGlossaryIsNotPresent(t *testing.T) {
	g := Load(t.TempDir())
	if g.Present {
		t.Error("absent docs/glossary.md should not be Present")
	}
	if len(g.Terms) != 0 {
		t.Error("absent glossary should have no terms")
	}
}

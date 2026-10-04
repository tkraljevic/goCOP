package main

import (
	"fmt"
	"go/scanner"
	"go/token"
	"hash/fnv"
	"sort"
	"strings"
)

type lexicalToken struct {
	Kind token.Token
	Text string
}
type functionTokens struct {
	ID     string
	Tokens []lexicalToken
}

func scanTokens(data []byte) ([]lexicalToken, error) {
	var s scanner.Scanner
	var scanErr error
	f := token.NewFileSet().AddFile("body", -1, len(data))
	s.Init(f, data, func(_ token.Position, msg string) { scanErr = fmt.Errorf("go token: %s", msg) }, 0)
	var tokens []lexicalToken
	for {
		_, kind, lit := s.Scan()
		if kind == token.EOF {
			break
		}
		if kind == token.SEMICOLON {
			lit = ";"
		}
		if lit == "" {
			lit = kind.String()
		}
		tokens = append(tokens, lexicalToken{kind, lit})
	}
	return tokens, scanErr
}

func tokenHash(t lexicalToken) uint64 {
	h := fnv.New64a()
	_, _ = fmt.Fprintf(h, "%d:%s", t.Kind, t.Text)
	return h.Sum64()
}

type occurrence struct{ function, index int }

func equalWindow(a, b []lexicalToken) bool {
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// Exact means identical lexical tokens (comments/spacing ignored), NOT dupl's
// normalized AST. Every duplicated token is counted once, including originals.
func exactClones(functions []functionTokens, minimum int) duplication {
	d := duplication{}
	index := map[uint64][]occurrence{}
	marked := make([][]bool, len(functions))
	pairSeen := map[string]bool{}
	const base uint64 = 1099511628211
	power := uint64(1)
	for i := 1; i < minimum; i++ {
		power *= base
	}
	for fi, f := range functions {
		d.TotalTokens += len(f.Tokens)
		marked[fi] = make([]bool, len(f.Tokens))
		if len(f.Tokens) < minimum {
			continue
		}
		hashes := make([]uint64, len(f.Tokens))
		for i, t := range f.Tokens {
			hashes[i] = tokenHash(t)
		}
		var h uint64
		for i, v := range hashes {
			if i >= minimum {
				h -= hashes[i-minimum] * power
			}
			h = h*base + v
			if i+1 < minimum {
				continue
			}
			start := i + 1 - minimum
			for _, prev := range index[h] {
				if prev.function == fi && start-prev.index < minimum {
					continue
				}
				old := functions[prev.function]
				if !equalWindow(f.Tokens[start:i+1], old.Tokens[prev.index:prev.index+minimum]) {
					continue
				}
				for k := 0; k < minimum; k++ {
					marked[fi][start+k] = true
					marked[prev.function][prev.index+k] = true
				}
				key := old.ID + "\x00" + f.ID
				if !pairSeen[key] && len(d.Pairs) < 200 {
					d.Pairs = append(d.Pairs, clone{old.ID, f.ID, 1, minimum})
					pairSeen[key] = true
				}
			}
			index[h] = append(index[h], occurrence{fi, start})
		}
	}
	for _, marks := range marked {
		for _, v := range marks {
			if v {
				d.DuplicatedTokens++
			}
		}
	}
	if d.TotalTokens > 0 {
		d.Percent = 100 * float64(d.DuplicatedTokens) / float64(d.TotalTokens)
	}
	return d
}

func shingles(tokens []lexicalToken) map[string]bool {
	words := make([]string, len(tokens))
	names := map[string]string{}
	for i, t := range tokens {
		word := t.Text
		// Alpha-renaming preserves repeated-variable relationships. Selectors and
		// literals stay intact; this is a syntactic heuristic, not equivalence.
		if t.Kind == token.IDENT && (i == 0 || tokens[i-1].Kind != token.PERIOD) {
			if _, ok := names[word]; !ok {
				names[word] = fmt.Sprintf("$%d", len(names))
			}
			word = names[word]
		}
		words[i] = word
	}
	set := map[string]bool{}
	for i := 0; i+5 <= len(words); i++ {
		set[strings.Join(words[i:i+5], "\x00")] = true
	}
	return set
}

func similarity(a, b map[string]bool) float64 {
	if len(a) > len(b) {
		a, b = b, a
	}
	common := 0
	for s := range a {
		if b[s] {
			common++
		}
	}
	union := len(a) + len(b) - common
	if union == 0 {
		return 0
	}
	return float64(common) / float64(union)
}

func fuzzyClones(functions []functionTokens, minimum int, threshold float64) duplication {
	d := duplication{}
	sets := make([]map[string]bool, len(functions))
	marked := make([]bool, len(functions))
	for i, f := range functions {
		d.TotalTokens += len(f.Tokens)
		if len(f.Tokens) >= minimum {
			sets[i] = shingles(f.Tokens)
		}
	}
	for i, a := range functions {
		if sets[i] == nil {
			continue
		}
		for j := 0; j < i; j++ {
			b := functions[j]
			if sets[j] == nil {
				continue
			}
			// Jaccard cannot exceed the ratio of set cardinalities.
			lo, hi := len(sets[i]), len(sets[j])
			if lo > hi {
				lo, hi = hi, lo
			}
			if float64(lo)/float64(hi) < threshold {
				continue
			}
			s := similarity(sets[i], sets[j])
			if s < threshold {
				continue
			}
			marked[i], marked[j] = true, true
			d.Pairs = append(d.Pairs, clone{b.ID, a.ID, s, min(len(a.Tokens), len(b.Tokens))})
		}
	}
	for i, v := range marked {
		if v {
			d.DuplicatedTokens += len(functions[i].Tokens)
		}
	}
	if d.TotalTokens > 0 {
		d.Percent = 100 * float64(d.DuplicatedTokens) / float64(d.TotalTokens)
	}
	sort.Slice(d.Pairs, func(i, j int) bool {
		if d.Pairs[i].Similarity != d.Pairs[j].Similarity {
			return d.Pairs[i].Similarity > d.Pairs[j].Similarity
		}
		return d.Pairs[i].A+d.Pairs[i].B < d.Pairs[j].A+d.Pairs[j].B
	})
	if len(d.Pairs) > 200 {
		d.Pairs = d.Pairs[:200]
	}
	return d
}

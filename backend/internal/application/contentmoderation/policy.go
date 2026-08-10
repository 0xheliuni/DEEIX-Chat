package contentmoderation

import (
	"sort"
	"strings"
)

// Policy holds the four category arrays (empty array = skip that surface).
type Policy struct {
	InputTextCategories   []string
	OutputTextCategories  []string
	InputImageCategories  []string
	OutputImageCategories []string
	Version               int64
}

type policyJSON struct {
	InputTextCategories   []string `json:"inputTextCategories"`
	OutputTextCategories  []string `json:"outputTextCategories"`
	InputImageCategories  []string `json:"inputImageCategories"`
	OutputImageCategories []string `json:"outputImageCategories"`
	Version               int64    `json:"version"`
}

func newPolicyJSON(policy Policy) policyJSON {
	return policyJSON{
		InputTextCategories:   policy.InputTextCategories,
		OutputTextCategories:  policy.OutputTextCategories,
		InputImageCategories:  policy.InputImageCategories,
		OutputImageCategories: policy.OutputImageCategories,
		Version:               policy.Version,
	}
}

func (document policyJSON) toPolicy() Policy {
	return Policy{
		InputTextCategories:   document.InputTextCategories,
		OutputTextCategories:  document.OutputTextCategories,
		InputImageCategories:  document.InputImageCategories,
		OutputImageCategories: document.OutputImageCategories,
		Version:               document.Version,
	}
}

// Enabled reports whether any surface has categories selected.
func (p Policy) Enabled() bool {
	return len(p.InputTextCategories) > 0 ||
		len(p.OutputTextCategories) > 0 ||
		len(p.InputImageCategories) > 0 ||
		len(p.OutputImageCategories) > 0
}

// CategoriesFor returns selected categories for a direction+modality surface.
func (p Policy) CategoriesFor(direction, modality string) []string {
	switch {
	case direction == DirectionInput && modality == ModalityText:
		return append([]string(nil), p.InputTextCategories...)
	case direction == DirectionOutput && modality == ModalityText:
		return append([]string(nil), p.OutputTextCategories...)
	case direction == DirectionInput && modality == ModalityImage:
		return append([]string(nil), p.InputImageCategories...)
	case direction == DirectionOutput && modality == ModalityImage:
		return append([]string(nil), p.OutputImageCategories...)
	default:
		return nil
	}
}

// NormalizePolicy validates and normalizes category selections.
func NormalizePolicy(p Policy) (Policy, error) {
	out := Policy{Version: p.Version}
	var err error
	if out.InputTextCategories, err = normalizeTextCategories(p.InputTextCategories); err != nil {
		return Policy{}, err
	}
	if out.OutputTextCategories, err = normalizeTextCategories(p.OutputTextCategories); err != nil {
		return Policy{}, err
	}
	if out.InputImageCategories, err = normalizeImageCategories(p.InputImageCategories); err != nil {
		return Policy{}, err
	}
	if out.OutputImageCategories, err = normalizeImageCategories(p.OutputImageCategories); err != nil {
		return Policy{}, err
	}
	return out, nil
}

func normalizeTextCategories(raw []string) ([]string, error) {
	return normalizeCategories(raw, false)
}

func normalizeImageCategories(raw []string) ([]string, error) {
	return normalizeCategories(raw, true)
}

func normalizeCategories(raw []string, imageOnly bool) ([]string, error) {
	if len(raw) == 0 {
		return []string{}, nil
	}
	seen := make(map[string]struct{}, len(raw))
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		cat := strings.TrimSpace(item)
		if cat == "" {
			continue
		}
		if !IsKnownCategory(cat) {
			return nil, ErrInvalidCategories
		}
		if imageOnly {
			if IsTextOnlyCategory(cat) || !IsImageCategory(cat) {
				return nil, ErrImageTextOnlyCategory
			}
		}
		if _, ok := seen[cat]; ok {
			continue
		}
		seen[cat] = struct{}{}
		out = append(out, cat)
	}
	sort.Strings(out)
	return out, nil
}

// HitEvaluation is the policy-aware decision for one API response.
type HitEvaluation struct {
	Hit        bool
	Categories []string
	Scores     map[string]float64
}

// EvaluateHit decides a block using only selected categories that are true
// and whose applied input types include the expected modality.
// Top-level flagged is intentionally ignored.
func EvaluateHit(resp *Response, selected []string, expectedModality string) HitEvaluation {
	result := HitEvaluation{
		Categories: make([]string, 0),
		Scores:     make(map[string]float64),
	}
	if resp == nil || len(selected) == 0 {
		return result
	}
	selectedSet := make(map[string]struct{}, len(selected))
	for _, cat := range selected {
		selectedSet[cat] = struct{}{}
	}
	expected := modalityToken(expectedModality)
	for _, item := range resp.Results {
		for cat := range selectedSet {
			if !item.Categories[cat] {
				continue
			}
			if !categoryAppliesToModality(item.CategoryAppliedInputTypes, cat, expected) {
				continue
			}
			if _, exists := result.Scores[cat]; !exists {
				result.Categories = append(result.Categories, cat)
				if item.CategoryScores != nil {
					result.Scores[cat] = item.CategoryScores[cat]
				}
			}
		}
	}
	if len(result.Categories) > 0 {
		sort.Strings(result.Categories)
		result.Hit = true
	}
	return result
}

func modalityToken(modality string) string {
	switch strings.TrimSpace(modality) {
	case ModalityImage:
		return "image"
	default:
		return "text"
	}
}

func categoryAppliesToModality(applied map[string][]string, category, expected string) bool {
	if applied == nil {
		// Some proxies omit applied types; accept the category hit.
		return true
	}
	types, ok := applied[category]
	if !ok || len(types) == 0 {
		// When the field is present but category has no types, still accept.
		return true
	}
	for _, t := range types {
		if strings.EqualFold(strings.TrimSpace(t), expected) {
			return true
		}
	}
	return false
}

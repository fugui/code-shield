package debate

import (
	"code-shield/models"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

const (
	CategoryResolutionValid = iota + 1
	CategoryResolutionCodeAuthority
	CategoryResolutionLabelAuthority
	CategoryResolutionAliasAuthority
	CategoryResolutionNeedsRepair
)

const (
	CategorySourceDeterministic = "deterministic"
	CategorySourceJudge         = "judge"
	CategorySourceJudgeInherit  = "judge_inherit"
)

const (
	CategoryStatusValid          = "VALID"
	CategoryStatusReviewRequired = "REVIEW_REQUIRED"
	CategoryStatusLegacy         = "LEGACY"
)

type CategoryResolution struct {
	Status     int
	Authority  string
	Code       string
	Label      string
	AliasKind  string
	AliasLabel string
}

func ResolveCategory(rawCode string, rawLabel string, taxonomy models.CategoryTaxonomy) CategoryResolution {
	code := normalizeCategoryToken(rawCode)
	label := normalizeCategoryToken(rawLabel)
	if code == "" && label == "" {
		return CategoryResolution{Status: CategoryResolutionNeedsRepair, Authority: "none"}
	}

	definitions := make(map[string]models.CategoryDefinition, len(taxonomy.Categories))
	labels := make(map[string]models.CategoryDefinition, len(taxonomy.Categories))
	deprecatedAliases := make(map[string]int, len(taxonomy.DeprecatedAliases))
	inlineAliases := make(map[string]int, len(taxonomy.Categories))
	for _, definition := range taxonomy.Categories {
		if definition.Code == "" || definition.Label == "" {
			continue
		}
		codeKey := normalizeCategoryToken(definition.Code)
		labelKey := normalizeCategoryToken(definition.Label)
		definitions[codeKey] = definition
		labels[labelKey] = definition
		for _, alias := range definition.Aliases {
			inlineAliases[normalizeCategoryToken(alias.Label)]++
		}
	}
	for _, alias := range taxonomy.DeprecatedAliases {
		deprecatedAliases[normalizeCategoryToken(alias.Label)]++
	}

	if definition, ok := definitions[code]; ok {
		if label == "" {
			return resolution(CategoryResolutionCodeAuthority, "code", definition)
		}
		if label == normalizeCategoryToken(definition.Label) {
			return resolution(CategoryResolutionValid, "code_and_label", definition)
		}
		return resolution(CategoryResolutionCodeAuthority, "code", definition)
	}

	if definition, ok := labels[label]; ok {
		if code == "" {
			return resolution(CategoryResolutionLabelAuthority, "label", definition)
		}
	}

	for _, alias := range taxonomy.DeprecatedAliases {
		aliasKey := normalizeCategoryToken(alias.Label)
		if aliasKey != label {
			continue
		}
		if deprecatedAliases[aliasKey] > 1 || inlineAliases[aliasKey] > 0 {
			return CategoryResolution{Status: CategoryResolutionNeedsRepair, Authority: "none", Code: rawCode, Label: rawLabel}
		}
		if definition, ok := definitions[normalizeCategoryToken(alias.TargetCode)]; ok {
			result := resolution(CategoryResolutionAliasAuthority, "deprecated_alias", definition)
			result.AliasKind = "deprecated"
			result.AliasLabel = alias.Label
			return result
		}
	}
	for _, definition := range taxonomy.Categories {
		for _, alias := range definition.Aliases {
			aliasKey := normalizeCategoryToken(alias.Label)
			if aliasKey != label {
				continue
			}
			if inlineAliases[aliasKey] > 1 || deprecatedAliases[aliasKey] > 0 {
				return CategoryResolution{Status: CategoryResolutionNeedsRepair, Authority: "none", Code: rawCode, Label: rawLabel}
			}
			result := resolution(CategoryResolutionAliasAuthority, "inline_alias", definition)
			result.AliasKind = "inline"
			result.AliasLabel = alias.Label
			return result
		}
	}

	return CategoryResolution{Status: CategoryResolutionNeedsRepair, Authority: "none", Code: rawCode, Label: rawLabel}
}

func resolution(status int, authority string, definition models.CategoryDefinition) CategoryResolution {
	return CategoryResolution{Status: status, Authority: authority, Code: definition.Code, Label: definition.Label}
}

func normalizeCategoryToken(value string) string {
	normalized := norm.NFKC.String(strings.TrimSpace(value))
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return ' '
		}
		return unicode.ToLower(r)
	}, normalized)
}

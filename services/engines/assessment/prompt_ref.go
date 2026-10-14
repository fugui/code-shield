package assessment

import (
	"fmt"
	"sort"
	"strings"

	"code-shield/services/coverage"
)

type PromptRef struct {
	Ref         string
	UnitID      string
	DisplayName string
}

type PromptRefSession struct {
	BundleID string
	Refs     []PromptRef
	byRef    map[string]PromptRef
	byID     map[string]PromptRef
}

func NewPromptRefSession(bundleID string, unitIDs []string, displayNames map[string]string) (*PromptRefSession, error) {
	if strings.TrimSpace(bundleID) == "" {
		return nil, fmt.Errorf("bundle id is empty")
	}
	session := &PromptRefSession{
		BundleID: bundleID,
		Refs:     make([]PromptRef, 0, len(unitIDs)),
		byRef:    make(map[string]PromptRef, len(unitIDs)),
		byID:     make(map[string]PromptRef, len(unitIDs)),
	}
	ordered := append([]string(nil), unitIDs...)
	sort.Strings(ordered)
	for index, unitID := range ordered {
		if unitID == "" {
			return nil, fmt.Errorf("primary unit id is empty")
		}
		if _, exists := session.byID[unitID]; exists {
			return nil, fmt.Errorf("primary unit id %q is duplicated", unitID)
		}
		ref := PromptRef{
			Ref:         fmt.Sprintf("u%03d", index+1),
			UnitID:      unitID,
			DisplayName: displayNames[unitID],
		}
		session.Refs = append(session.Refs, ref)
		session.byRef[ref.Ref] = ref
		session.byID[unitID] = ref
	}
	return session, nil
}

func (session *PromptRefSession) UnitIDForRef(ref string) (string, error) {
	promptRef, exists := session.byRef[ref]
	if !exists {
		return "", fmt.Errorf("%w: unknown unit_ref %q", ErrContractRepair, ref)
	}
	return promptRef.UnitID, nil
}

func (session *PromptRefSession) RefForUnitID(unitID string) (PromptRef, bool) {
	promptRef, exists := session.byID[unitID]
	return promptRef, exists
}

// SubSession creates a subset PromptRefSession preserving the original unit_ref mappings.
func (session *PromptRefSession) SubSession(bundleID string, units []coverage.PlanUnit) (*PromptRefSession, error) {
	if session == nil {
		return nil, fmt.Errorf("parent prompt ref session is nil")
	}
	if strings.TrimSpace(bundleID) == "" {
		bundleID = session.BundleID
	}
	sub := &PromptRefSession{
		BundleID: bundleID,
		Refs:     make([]PromptRef, 0, len(units)),
		byRef:    make(map[string]PromptRef, len(units)),
		byID:     make(map[string]PromptRef, len(units)),
	}
	for _, unit := range units {
		ref, exists := session.byID[unit.ID]
		if !exists {
			return nil, fmt.Errorf("%w: unit %q not found in parent prompt ref session", ErrContractRepair, unit.ID)
		}
		sub.Refs = append(sub.Refs, ref)
		sub.byRef[ref.Ref] = ref
		sub.byID[unit.ID] = ref
	}
	return sub, nil
}

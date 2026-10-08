package engines

import (
	"encoding/json"
	"fmt"

	"code-shield/services/engines/profile"
)

// ParsedEngineProfile is the validated execution view of a task's engine_config.
type ParsedEngineProfile struct {
	Profile profile.ScanProfile
	Hash    string
	Config  ChunkConfig
}

// ParseProfileConfig validates the nested profile SSOT and projects it onto the
// existing chunk execution configuration.
func ParseProfileConfig(raw json.RawMessage) (ParsedEngineProfile, error) {
	scanProfile, profileHash, err := profile.Parse(raw)
	if err != nil {
		return ParsedEngineProfile{}, fmt.Errorf("invalid engine_config: %w", err)
	}

	config := ChunkConfig{
		MaxFiles:        scanProfile.MaxFiles,
		Depth:           scanProfile.Depth,
		FileExtensions:  append([]string(nil), scanProfile.IncludeExtensions...),
		ContentKeywords: append([]string(nil), scanProfile.ContentKeywords...),
		ExcludePaths:    append([]string(nil), scanProfile.ExcludePaths...),
	}
	if config.MaxFiles <= 0 {
		config.MaxFiles = DefaultChunkMaxFiles
	}
	if config.Depth <= 0 {
		config.Depth = DefaultChunkDepth
	}
	if scanProfile.BasePolicy != nil {
		config.SinceDays = scanProfile.BasePolicy.SinceDays
	}

	return ParsedEngineProfile{Profile: scanProfile, Hash: profileHash, Config: config}, nil
}

// ParseProfileSnapshot validates the canonical profile stored on TaskReport.
// The snapshot contains only ScanProfile; ParsedEngineProfile is a runtime view
// and must never be persisted as the report snapshot.
func ParseProfileSnapshot(raw json.RawMessage) (ParsedEngineProfile, error) {
	var scanProfile profile.ScanProfile
	if err := json.Unmarshal(raw, &scanProfile); err != nil {
		return ParsedEngineProfile{}, fmt.Errorf("invalid scan profile snapshot: %w", err)
	}
	engineConfig, err := json.Marshal(profile.EngineConfig{ScanProfile: scanProfile})
	if err != nil {
		return ParsedEngineProfile{}, fmt.Errorf("encode scan profile snapshot: %w", err)
	}
	parsedProfile, err := ParseProfileConfig(engineConfig)
	if err != nil {
		return ParsedEngineProfile{}, fmt.Errorf("invalid scan profile snapshot: %w", err)
	}
	return parsedProfile, nil
}

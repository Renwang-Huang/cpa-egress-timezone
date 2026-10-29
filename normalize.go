package main

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
	_ "time/tzdata"
)

var (
	environmentBlock = regexp.MustCompile(`(?s)<environment_context>.*?</environment_context>`)
	timezoneTag      = regexp.MustCompile(`(<timezone>)([^<]*)(</timezone>)`)
	currentDateTag   = regexp.MustCompile(`(<current_date>)([^<]*)(</current_date>)`)
)

// Each payload is visited newest first so only the latest client environment
// can have its current date adjusted. Older environment dates remain intact.
type normalizationState struct {
	now            time.Time
	sawEnvironment bool
	zone           string
	location       *time.Location
}

// normalizeBody edits client environment declarations and location metadata.
// RawMessage preserves opaque reasoning, tool arguments, and numbers.
func normalizeBodyInZone(raw []byte, now time.Time, zone string) ([]byte, error) {
	if len(raw) == 0 {
		return raw, nil
	}
	location, err := loadZone(zone)
	if err != nil {
		return nil, err
	}
	var root map[string]json.RawMessage
	if err := json.Unmarshal(raw, &root); err != nil {
		return nil, fmt.Errorf("decode request body: %w", err)
	}
	changed := rewritePayload(root, &normalizationState{now: now, zone: zone, location: location})
	if !changed {
		return raw, nil
	}
	return json.Marshal(root)
}

func rewritePayload(root map[string]json.RawMessage, state *normalizationState) bool {
	changed := rewriteTimezoneFields(root, state)
	// Later messages and configuration updates override initial instructions.
	for _, key := range []string{"messages", "input"} {
		if rewriteMessageArray(root, key, state) {
			changed = true
		}
	}
	for _, key := range []string{"input", "system", "instructions"} {
		if rewriteStringField(root, key, state) {
			changed = true
		}
	}
	for _, key := range []string{"client_metadata", "metadata", "user_location", "environment_context", "client_context"} {
		if rewriteContextObject(root, key, state) {
			changed = true
		}
	}
	var tools []map[string]json.RawMessage
	if json.Unmarshal(root["tools"], &tools) == nil {
		toolsChanged := false
		for _, tool := range tools {
			var kind string
			_ = json.Unmarshal(tool["type"], &kind)
			if strings.HasPrefix(kind, "web_search") && rewriteContextObject(tool, "user_location", state) {
				toolsChanged = true
			}
		}
		if toolsChanged {
			root["tools"], _ = json.Marshal(tools)
			changed = true
		}
	}
	return changed
}

func rewriteTimezoneFields(obj map[string]json.RawMessage, state *normalizationState) bool {
	changed := false
	for key, raw := range obj {
		canonical := strings.ToLower(strings.NewReplacer("_", "", "-", "").Replace(key))
		switch canonical {
		case "timezone", "clienttimezone", "usertimezone", "tz":
			var value *string
			if json.Unmarshal(raw, &value) == nil && value != nil && *value != state.zone {
				obj[key], _ = json.Marshal(state.zone)
				changed = true
			}
		}
	}
	return changed
}

func rewriteContextObject(parent map[string]json.RawMessage, key string, state *normalizationState) bool {
	var obj map[string]json.RawMessage
	if json.Unmarshal(parent[key], &obj) != nil || obj == nil {
		return false
	}
	changed := rewriteTimezoneFields(obj, state)
	for _, child := range []string{"user_location", "location", "environment_context", "client_context"} {
		if rewriteContextObject(obj, child, state) {
			changed = true
		}
	}
	if changed {
		parent[key], _ = json.Marshal(obj)
	}
	return changed
}

func rewriteStringField(obj map[string]json.RawMessage, key string, state *normalizationState) bool {
	var text string
	if json.Unmarshal(obj[key], &text) != nil {
		return false
	}
	updated := text
	matches := environmentBlock.FindAllStringIndex(text, -1)
	for i := len(matches) - 1; i >= 0; i-- {
		start, end := matches[i][0], matches[i][1]
		block := text[start:end]
		if !state.sawEnvironment {
			state.sawEnvironment = true
			block = rewriteActiveDate(block, state.now, state.location)
		}
		block = timezoneTag.ReplaceAllString(block, "${1}"+state.zone+"${3}")
		updated = updated[:start] + block + updated[end:]
	}
	if updated == text {
		return false
	}
	obj[key], _ = json.Marshal(updated)
	return true
}

// A calendar date has no unique instant to convert. Only treat the latest
// declaration as current when its date equals today in its original IANA zone.
// Historical, ambiguous, missing, and invalid declarations keep their dates.
func rewriteActiveDate(block string, now time.Time, targetLocation *time.Location) string {
	zones := timezoneTag.FindAllStringSubmatch(block, -1)
	dates := currentDateTag.FindAllStringSubmatch(block, -1)
	if len(zones) != 1 || len(dates) != 1 {
		return block
	}
	sourceZone := strings.TrimSpace(zones[0][2])
	if sourceZone == "" || sourceZone == "Local" {
		return block
	}
	sourceLocation, err := time.LoadLocation(sourceZone)
	if err != nil || strings.TrimSpace(dates[0][2]) != now.In(sourceLocation).Format("2006-01-02") {
		return block
	}
	return currentDateTag.ReplaceAllString(block, "${1}"+now.In(targetLocation).Format("2006-01-02")+"${3}")
}

func rewriteMessageArray(root map[string]json.RawMessage, key string, state *normalizationState) bool {
	var items []map[string]json.RawMessage
	if json.Unmarshal(root[key], &items) != nil {
		return false
	}
	changed := false
	for i := len(items) - 1; i >= 0; i-- {
		item := items[i]
		var kind, role string
		_ = json.Unmarshal(item["type"], &kind)
		_ = json.Unmarshal(item["role"], &role)
		if kind == "configuration_update" {
			var configuration map[string]json.RawMessage
			if json.Unmarshal(item["configuration"], &configuration) == nil && rewritePayload(configuration, state) {
				item["configuration"], _ = json.Marshal(configuration)
				changed = true
			}
			continue
		}
		if role != "user" && role != "system" && role != "developer" {
			continue
		}
		if rewriteStringField(item, "content", state) {
			changed = true
			continue
		}
		var parts []map[string]json.RawMessage
		if json.Unmarshal(item["content"], &parts) != nil {
			continue
		}
		partsChanged := false
		for p := len(parts) - 1; p >= 0; p-- {
			part := parts[p]
			var partType string
			_ = json.Unmarshal(part["type"], &partType)
			if (partType == "input_text" || partType == "text") && rewriteStringField(part, "text", state) {
				partsChanged = true
			}
		}
		if partsChanged {
			item["content"], _ = json.Marshal(parts)
			changed = true
		}
	}
	if changed {
		root[key], _ = json.Marshal(items)
	}
	return changed
}

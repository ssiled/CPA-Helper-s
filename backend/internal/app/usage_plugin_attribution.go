package app

import (
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	pluginUsageAttributionWindow    = 2 * time.Minute
	pluginUsageAttributionAmbiguity = time.Second
)

type pluginUsageOwnerRepair struct {
	ID        int
	Timestamp time.Time
	RequestID string
	Provider  string
	Model     string
	AuthIndex string
	LatencyMS *float64
}

type pluginUsageAttributionEdge struct {
	eventKey   string
	apiKeyHash string
	distance   time.Duration
}

type pluginUsageAttributionGroup struct {
	records []pluginUsageOwnerRepair
	edges   []pluginUsageAttributionEdge
}

func matchPluginUsageAttributions(records []pluginUsageOwnerRepair, events []authPoolPluginEvent) map[int]string {
	groupsByKey := make(map[string]*pluginUsageAttributionGroup, len(records))
	groups := make([]*pluginUsageAttributionGroup, 0, len(records))
	for _, record := range records {
		key := strings.TrimSpace(record.RequestID)
		if key == "" || key == "-" {
			key = "record:" + strconv.Itoa(record.ID)
		} else {
			key = "request:" + key
		}
		group := groupsByKey[key]
		if group == nil {
			group = &pluginUsageAttributionGroup{}
			groupsByKey[key] = group
			groups = append(groups, group)
		}
		group.records = append(group.records, record)
	}

	for _, group := range groups {
		bestByEvent := map[string]pluginUsageAttributionEdge{}
		for _, record := range group.records {
			for _, event := range events {
				edge, ok := pluginUsageAttributionEdgeFor(record, event)
				if !ok {
					continue
				}
				if current, exists := bestByEvent[edge.eventKey]; !exists || edge.distance < current.distance {
					bestByEvent[edge.eventKey] = edge
				}
			}
		}
		for _, edge := range bestByEvent {
			group.edges = append(group.edges, edge)
		}
		sort.Slice(group.edges, func(i, j int) bool {
			if group.edges[i].distance == group.edges[j].distance {
				return group.edges[i].eventKey < group.edges[j].eventKey
			}
			return group.edges[i].distance < group.edges[j].distance
		})
	}
	sort.SliceStable(groups, func(i, j int) bool {
		if len(groups[i].edges) == 0 {
			return false
		}
		if len(groups[j].edges) == 0 {
			return true
		}
		return groups[i].edges[0].distance < groups[j].edges[0].distance
	})

	matched := map[int]string{}
	usedEvents := map[string]struct{}{}
	for _, group := range groups {
		available := make([]pluginUsageAttributionEdge, 0, len(group.edges))
		for _, edge := range group.edges {
			if _, used := usedEvents[edge.eventKey]; !used {
				available = append(available, edge)
			}
		}
		if len(available) == 0 {
			continue
		}
		best := available[0]
		ambiguous := false
		for _, edge := range available[1:] {
			if edge.distance > best.distance+pluginUsageAttributionAmbiguity {
				break
			}
			if edge.apiKeyHash != best.apiKeyHash {
				ambiguous = true
				break
			}
		}
		if ambiguous {
			continue
		}
		usedEvents[best.eventKey] = struct{}{}
		for _, record := range group.records {
			matched[record.ID] = best.apiKeyHash
		}
	}
	return matched
}

func pluginUsageAttributionEdgeFor(record pluginUsageOwnerRepair, event authPoolPluginEvent) (pluginUsageAttributionEdge, bool) {
	apiKeyHash := strings.TrimSpace(event.APIKeyHash)
	if apiKeyHash == "" || !pluginUsageAuthIDMatches(record.AuthIndex, event.SelectedAuthID) {
		return pluginUsageAttributionEdge{}, false
	}
	phase := strings.ToLower(strings.TrimSpace(event.Phase))
	if phase == "selection" {
		if !strings.EqualFold(strings.TrimSpace(event.Status), "selected") {
			return pluginUsageAttributionEdge{}, false
		}
	} else if phase != "completion" {
		return pluginUsageAttributionEdge{}, false
	}
	if phase == "completion" && event.AttributionID == 0 {
		return pluginUsageAttributionEdge{}, false
	}
	if !modelProxyAttributionModelMatches(event.Model, record.Model) || !pluginUsageProviderMatches(event.Provider, record.Provider) {
		return pluginUsageAttributionEdge{}, false
	}
	eventTime, ok := parseDBTime(event.Timestamp)
	if !ok || record.Timestamp.IsZero() {
		return pluginUsageAttributionEdge{}, false
	}
	reference := record.Timestamp
	if phase == "completion" && record.LatencyMS != nil && *record.LatencyMS > 0 {
		latency := time.Duration(*record.LatencyMS * float64(time.Millisecond))
		if latency <= pluginUsageAttributionWindow {
			reference = reference.Add(latency)
		}
	}
	distance := absDuration(eventTime.Sub(reference))
	if distance > pluginUsageAttributionWindow {
		return pluginUsageAttributionEdge{}, false
	}
	eventIdentity := "event:" + strconv.FormatUint(event.ID, 10) + ":" + event.Timestamp + ":" + phase
	if event.AttributionID != 0 {
		eventIdentity = "attribution:" + strconv.FormatUint(event.AttributionID, 10)
	}
	eventKey := event.TargetID + ":" + eventIdentity
	return pluginUsageAttributionEdge{eventKey: eventKey, apiKeyHash: apiKeyHash, distance: distance}, true
}

func pluginUsageAuthIDMatches(left, right string) bool {
	left = normalizePluginUsageAuthID(left)
	right = normalizePluginUsageAuthID(right)
	return left != "" && right != "" && left == right
}

func normalizePluginUsageAuthID(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, "\\", "/")
	if index := strings.LastIndex(value, "/"); index >= 0 {
		value = value[index+1:]
	}
	var builder strings.Builder
	lastUnderscore := false
	for _, char := range value {
		isLetter := char >= 'a' && char <= 'z'
		isDigit := char >= '0' && char <= '9'
		if isLetter || isDigit {
			builder.WriteRune(char)
			lastUnderscore = false
			continue
		}
		if builder.Len() > 0 && !lastUnderscore {
			builder.WriteByte('_')
			lastUnderscore = true
		}
	}
	result := strings.Trim(builder.String(), "_")
	for _, prefix := range []string{"root_cli_proxy_api_", "root_cli_proxy_", "cli_proxy_api_"} {
		result = strings.TrimPrefix(result, prefix)
	}
	for _, suffix := range []string{"_json", "_yaml", "_yml", "_toml"} {
		result = strings.TrimSuffix(result, suffix)
	}
	return result
}

func pluginUsageProviderMatches(left, right string) bool {
	left = normalizePluginUsageProvider(left)
	right = normalizePluginUsageProvider(right)
	return left == "" || right == "" || left == right
}

func normalizePluginUsageProvider(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if strings.HasPrefix(value, "openai-compatible") || strings.HasPrefix(value, "openai_compatible") {
		return "openai-compatible"
	}
	return value
}

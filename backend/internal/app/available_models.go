package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"
)

const modelListTimeout = 8 * time.Second

var (
	modelContainerKeys       = []string{"data", "models", "items", "value"}
	modelIDKeys              = []string{"id", "model", "name"}
	modelOwnerKeys           = []string{"owner", "owned_by", "organization"}
	modelReservedMetadataKey = map[string]bool{
		"id": true, "model": true, "name": true,
		"owner": true, "owned_by": true, "organization": true,
		"object": true, "created": true,
	}
)

type AvailableModelSource struct {
	APIKeyHash    string `json:"api_key_hash"`
	APIKeyPreview string `json:"api_key_preview"`
	Description   string `json:"description"`
	UserID        *int   `json:"user_id,omitempty"`
	UserLabel     string `json:"user_label,omitempty"`
}

type AvailableModelPrice struct {
	Provider                   string   `json:"provider"`
	Model                      string   `json:"model"`
	InputUSDPerMillion         float64  `json:"input_usd_per_million"`
	OutputUSDPerMillion        float64  `json:"output_usd_per_million"`
	CacheReadUSDPerMillion     float64  `json:"cache_read_usd_per_million"`
	CacheCreationUSDPerMillion float64  `json:"cache_creation_usd_per_million"`
	RequestUSD                 *float64 `json:"request_usd"`
	BillingUnit                string   `json:"billing_unit"`
}

type AvailableModelItem struct {
	ID       string                 `json:"id"`
	Name     string                 `json:"name"`
	Object   *string                `json:"object"`
	Owner    *string                `json:"owner"`
	Created  *int                   `json:"created"`
	Metadata map[string]any         `json:"metadata"`
	Price    *AvailableModelPrice   `json:"price"`
	Sources  []AvailableModelSource `json:"sources"`
}

type AvailableModelKeyError struct {
	APIKeyHash    string `json:"api_key_hash"`
	APIKeyPreview string `json:"api_key_preview"`
	Description   string `json:"description"`
	Message       string `json:"message"`
}

type AvailableModelsResponse struct {
	HasAPIKeys           bool                     `json:"has_api_keys"`
	APIKeyCount          int                      `json:"api_key_count"`
	QueryableAPIKeyCount int                      `json:"queryable_api_key_count"`
	Models               []AvailableModelItem     `json:"models"`
	Errors               []AvailableModelKeyError `json:"errors"`
}

func (a *App) handleAvailableModels(w http.ResponseWriter, r *http.Request) error {
	if err := requireMethod(r, http.MethodGet); err != nil {
		return err
	}
	user, err := a.readyUser(r.Context(), r)
	if err != nil {
		return err
	}
	response, err := a.availableModelsForUser(r.Context(), user.ID)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, response)
	return nil
}

func (a *App) availableModelsForUser(ctx context.Context, userID int) (AvailableModelsResponse, error) {
	cfg, err := a.loadConfig(ctx)
	if err != nil {
		return AvailableModelsResponse{}, err
	}
	if target, ok := activeAuthPoolProxyTarget(cfg); ok && strings.TrimSpace(target.APIKey) != "" {
		return a.availableModelsForPluginCPAKey(ctx, target)
	}
	bindings, err := a.userAPIKeys(ctx, userID)
	if err != nil {
		return AvailableModelsResponse{}, err
	}
	response := AvailableModelsResponse{
		HasAPIKeys:  len(bindings) > 0,
		APIKeyCount: len(bindings),
		Models:      []AvailableModelItem{},
		Errors:      []AvailableModelKeyError{},
	}
	if len(bindings) == 0 {
		return response, nil
	}
	queryable := make([]UserAPIKey, 0, len(bindings))
	for _, binding := range bindings {
		if binding.APIKey != nil && strings.TrimSpace(*binding.APIKey) != "" {
			queryable = append(queryable, binding)
		}
	}
	response.QueryableAPIKeyCount = len(queryable)
	if len(queryable) == 0 {
		return response, nil
	}
	apiKeyHashes := make([]string, 0, len(queryable))
	for _, binding := range queryable {
		apiKeyHashes = append(apiKeyHashes, binding.APIKeyHash)
	}
	poolModelFilters, err := a.authPoolModelFiltersForAPIKeys(ctx, apiKeyHashes)
	if err != nil {
		return AvailableModelsResponse{}, err
	}
	prices, err := a.priceMap(ctx)
	if err != nil {
		return AvailableModelsResponse{}, err
	}
	modelsByID := map[string]AvailableModelItem{}
	for _, binding := range queryable {
		source := availableModelSource(binding)
		models, err := fetchAvailableModelItemsForAPIKey(ctx, cfg, binding)
		if err != nil {
			response.Errors = append(response.Errors, AvailableModelKeyError{
				APIKeyHash:    source.APIKeyHash,
				APIKeyPreview: source.APIKeyPreview,
				Description:   source.Description,
				Message:       err.Error(),
			})
			continue
		}
		for _, raw := range models {
			model := parseAvailableModel(raw, source)
			if model == nil {
				continue
			}
			if !authPoolModelFilterAllows(poolModelFilters[binding.APIKeyHash], model.ID) {
				continue
			}
			existing, ok := modelsByID[model.ID]
			if !ok {
				modelsByID[model.ID] = *model
				continue
			}
			mergeAvailableModel(&existing, *model)
			modelsByID[model.ID] = existing
		}
	}
	if len(response.Errors) > 0 && len(modelsByID) == 0 {
		messages := make([]string, 0, len(response.Errors))
		for _, item := range response.Errors {
			messages = append(messages, item.Description+": "+item.Message)
			if len(messages) >= 3 {
				break
			}
		}
		return AvailableModelsResponse{}, validationError("查询 CPA 可用模型失败：" + strings.Join(messages, "；"))
	}
	appendPricedAvailableModels(&response, prices, modelsByID)
	return response, nil
}

func (a *App) availableModelsForPluginCPAKey(ctx context.Context, target AuthPoolProxyTargetConfig) (AvailableModelsResponse, error) {
	source := availableModelPluginSource()
	response := AvailableModelsResponse{
		HasAPIKeys:           true,
		APIKeyCount:          1,
		QueryableAPIKeyCount: 1,
		Models:               []AvailableModelItem{},
		Errors:               []AvailableModelKeyError{},
	}
	models, err := fetchAvailableModelItemsFromTarget(ctx, target.CPAURL, target.APIKey, "", false)
	if err != nil {
		message := safeAvailableModelError(err)
		response.Errors = append(response.Errors, AvailableModelKeyError{
			APIKeyHash:    source.APIKeyHash,
			APIKeyPreview: source.APIKeyPreview,
			Description:   source.Description,
			Message:       message,
		})
		return AvailableModelsResponse{}, validationError("查询 CPA 可用模型失败：" + source.Description + ": " + message)
	}
	prices, err := a.priceMap(ctx)
	if err != nil {
		return AvailableModelsResponse{}, err
	}
	modelsByID := map[string]AvailableModelItem{}
	for _, raw := range models {
		model := parseAvailableModel(raw, source)
		if model == nil {
			continue
		}
		existing, ok := modelsByID[model.ID]
		if !ok {
			modelsByID[model.ID] = *model
			continue
		}
		mergeAvailableModel(&existing, *model)
		modelsByID[model.ID] = existing
	}
	appendPricedAvailableModels(&response, prices, modelsByID)
	return response, nil
}

func appendPricedAvailableModels(response *AvailableModelsResponse, prices map[[2]string]ModelPrice, modelsByID map[string]AvailableModelItem) {
	for _, model := range modelsByID {
		if price := findMatchingPrice(prices, model.Owner, &model.ID); price != nil {
			model.Price = &AvailableModelPrice{
				Provider:                   price.Provider,
				Model:                      price.Model,
				InputUSDPerMillion:         price.InputUSDPerMillion,
				OutputUSDPerMillion:        price.OutputUSDPerMillion,
				CacheReadUSDPerMillion:     price.CacheReadUSDPerMillion,
				CacheCreationUSDPerMillion: price.CacheCreationUSDPerMillion,
				RequestUSD:                 price.RequestUSD,
				BillingUnit:                price.BillingUnit,
			}
		}
		response.Models = append(response.Models, model)
	}
	sort.Slice(response.Models, func(i, j int) bool {
		return strings.ToLower(response.Models[i].ID) < strings.ToLower(response.Models[j].ID)
	})
}

func availableModelPluginSource() AvailableModelSource {
	return AvailableModelSource{
		APIKeyHash:    "plugin-cpa-key",
		APIKeyPreview: "已配置",
		Description:   "CPA 插件 Key",
	}
}

func safeAvailableModelError(err error) string {
	if err == nil {
		return "CPA model list request failed"
	}
	message := strings.TrimSpace(err.Error())
	if message == "" {
		return "CPA model list request failed"
	}
	lower := strings.ToLower(message)
	if strings.Contains(message, "://") || strings.Contains(lower, "authorization") || strings.Contains(lower, "bearer ") || strings.Contains(lower, "api_key") || strings.Contains(lower, "api-key") {
		return "CPA model list request failed"
	}
	return message
}

func fetchAvailableModelItemsForAPIKey(ctx context.Context, cfg AppConfig, apiKey UserAPIKey) ([]any, error) {
	if apiKey.APIKey == nil {
		return nil, fmt.Errorf("API KEY unavailable")
	}
	target, upstreamAPIKey, includeHelperHash, err := modelProxyTarget(cfg, strings.TrimSpace(*apiKey.APIKey))
	if err != nil {
		return nil, err
	}
	return fetchAvailableModelItemsFromTarget(ctx, target.CPAURL, upstreamAPIKey, apiKey.APIKeyHash, includeHelperHash)
}

func fetchAvailableModelItems(ctx context.Context, cfg AppConfig, apiKey string) ([]any, error) {
	target, upstreamAPIKey, _, err := modelProxyTarget(cfg, apiKey)
	if err != nil {
		return nil, err
	}
	return fetchAvailableModelItemsFromTarget(ctx, target.CPAURL, upstreamAPIKey, "", false)
}

func fetchAvailableModelItemsFromTarget(ctx context.Context, cpaURL string, apiKey string, apiKeyHash string, includeHelperHash bool) ([]any, error) {
	headers := http.Header{}
	headers.Set("Authorization", "Bearer "+apiKey)
	if includeHelperHash {
		headers.Set(authPoolProxyAPIKeyHashHeader, strings.TrimSpace(apiKeyHash))
	}
	response, payload, err := doJSON(ctx, httpClient(modelListTimeout), http.MethodGet, makeURL(cpaURL, "/v1/models", nil), headers, nil)
	if err != nil {
		return nil, fmt.Errorf("CPA model list request failed: %s", err.Error())
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("CPA model list request failed: HTTP %d", response.StatusCode)
	}
	var raw any
	if err := json.Unmarshal(payload, &raw); err != nil {
		return nil, fmt.Errorf("CPA model list response is not valid JSON")
	}
	return extractAvailableModelItems(raw)
}

func extractAvailableModelItems(payload any) ([]any, error) {
	if items, ok := payload.([]any); ok {
		return items, nil
	}
	object, ok := payload.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("CPA 模型列表响应格式不支持")
	}
	for _, key := range modelContainerKeys {
		if value, exists := object[key]; exists {
			if items, ok := value.([]any); ok {
				return items, nil
			}
			return nil, fmt.Errorf("CPA 模型列表响应字段 %s 不是列表", key)
		}
	}
	for _, key := range modelIDKeys {
		if _, exists := object[key]; exists {
			return []any{object}, nil
		}
	}
	return nil, fmt.Errorf("CPA 模型列表响应缺少模型列表")
}

func parseAvailableModel(raw any, source AvailableModelSource) *AvailableModelItem {
	if text, ok := raw.(string); ok {
		modelID := strings.TrimSpace(text)
		if modelID == "" {
			return nil
		}
		return &AvailableModelItem{
			ID:       modelID,
			Name:     modelID,
			Metadata: map[string]any{},
			Sources:  []AvailableModelSource{source},
		}
	}
	object, ok := raw.(map[string]any)
	if !ok {
		return nil
	}
	modelID := firstStringValue(object, modelIDKeys)
	if modelID == nil {
		return nil
	}
	name := modelID
	if value := stringValue(object["name"]); value != nil {
		name = value
	}
	return &AvailableModelItem{
		ID:       *modelID,
		Name:     *name,
		Object:   stringValue(object["object"]),
		Owner:    firstStringValue(object, modelOwnerKeys),
		Created:  intValuePtr(object["created"]),
		Metadata: metadataFromModelObject(object),
		Sources:  []AvailableModelSource{source},
	}
}

func mergeAvailableModel(target *AvailableModelItem, incoming AvailableModelItem) {
	if target.Name == target.ID && incoming.Name != incoming.ID {
		target.Name = incoming.Name
	}
	if target.Object == nil {
		target.Object = incoming.Object
	}
	if target.Owner == nil {
		target.Owner = incoming.Owner
	}
	if target.Created == nil {
		target.Created = incoming.Created
	}
	if target.Metadata == nil {
		target.Metadata = map[string]any{}
	}
	for key, value := range incoming.Metadata {
		if _, exists := target.Metadata[key]; !exists {
			target.Metadata[key] = value
		}
	}
	seen := map[string]bool{}
	for _, source := range target.Sources {
		seen[source.APIKeyHash] = true
	}
	for _, source := range incoming.Sources {
		if !seen[source.APIKeyHash] {
			target.Sources = append(target.Sources, source)
		}
	}
	sort.Slice(target.Sources, func(i, j int) bool {
		left := strings.ToLower(target.Sources[i].Description) + target.Sources[i].APIKeyHash
		right := strings.ToLower(target.Sources[j].Description) + target.Sources[j].APIKeyHash
		return left < right
	})
}

func metadataFromModelObject(object map[string]any) map[string]any {
	metadata := map[string]any{}
	for key, value := range object {
		if modelReservedMetadataKey[key] || isSensitiveModelMetadataKey(key) || isSensitiveModelMetadataValue(value) || !isJSONScalar(value) {
			continue
		}
		metadata[key] = value
	}
	return metadata
}

func availableModelSource(binding UserAPIKey) AvailableModelSource {
	description := strings.TrimSpace(binding.Description)
	if description == "" {
		description = "未命名 Key"
	}
	return AvailableModelSource{
		APIKeyHash:    binding.APIKeyHash,
		APIKeyPreview: maskSecret(binding.APIKey),
		Description:   description,
	}
}

func firstStringValue(object map[string]any, keys []string) *string {
	for _, key := range keys {
		if value := stringValue(object[key]); value != nil {
			return value
		}
	}
	return nil
}

func stringValue(value any) *string {
	text, ok := value.(string)
	if !ok {
		return nil
	}
	normalized := strings.TrimSpace(text)
	if normalized == "" {
		return nil
	}
	return &normalized
}

func intValuePtr(value any) *int {
	switch typed := value.(type) {
	case float64:
		if typed < 0 {
			return nil
		}
		converted := int(typed)
		return &converted
	case int:
		if typed < 0 {
			return nil
		}
		return &typed
	default:
		return nil
	}
}

func isJSONScalar(value any) bool {
	switch value.(type) {
	case nil, string, float64, bool:
		return true
	default:
		return false
	}
}

func isSensitiveModelMetadataKey(key string) bool {
	lower := strings.ToLower(strings.TrimSpace(key))
	if lower == "" {
		return false
	}
	for _, marker := range []string{"key", "secret", "token", "authorization", "credential", "password", "passwd"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func isSensitiveModelMetadataValue(value any) bool {
	text, ok := value.(string)
	if !ok {
		return false
	}
	lower := strings.ToLower(strings.TrimSpace(text))
	if lower == "" {
		return false
	}
	return strings.Contains(lower, "bearer ") || strings.Contains(lower, "api_key") || strings.Contains(lower, "api-key") || strings.HasPrefix(lower, "sk-")
}

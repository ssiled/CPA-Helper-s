package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const defaultLiteLLMPricingURL = "https://raw.githubusercontent.com/BerriAI/litellm/main/model_prices_and_context_window.json"

type ModelPrice struct {
	ID                     int        `json:"id"`
	Provider               string     `json:"provider"`
	Model                  string     `json:"model"`
	InputUSDPerMillion     float64    `json:"input_usd_per_million"`
	OutputUSDPerMillion    float64    `json:"output_usd_per_million"`
	CachedUSDPerMillion    float64    `json:"cached_usd_per_million"`
	ReasoningUSDPerMillion float64    `json:"reasoning_usd_per_million"`
	Source                 string     `json:"source"`
	SourceModel            *string    `json:"source_model"`
	AutoSynced             bool       `json:"auto_synced"`
	LastSyncedAt           *time.Time `json:"last_synced_at"`
	UpdatedAt              time.Time  `json:"updated_at"`
}

type modelPricePayload struct {
	Provider               string  `json:"provider"`
	Model                  string  `json:"model"`
	InputUSDPerMillion     float64 `json:"input_usd_per_million"`
	OutputUSDPerMillion    float64 `json:"output_usd_per_million"`
	CachedUSDPerMillion    float64 `json:"cached_usd_per_million"`
	ReasoningUSDPerMillion float64 `json:"reasoning_usd_per_million"`
}

type modelPriceSyncRequest struct {
	SourceURL *string `json:"source_url"`
}

func (a *App) handleModelPrices(w http.ResponseWriter, r *http.Request) error {
	if _, err := a.adminUser(r.Context(), r); err != nil {
		return err
	}
	switch r.Method {
	case http.MethodGet:
		prices, err := a.listPrices(r.Context())
		if err != nil {
			return err
		}
		writeJSON(w, http.StatusOK, prices)
		return nil
	case http.MethodPost:
		if strings.HasSuffix(r.URL.Path, "/sync/litellm") {
			return a.handleSyncLiteLLMPrices(w, r)
		}
		var payload modelPricePayload
		if err := decodeJSON(r, &payload); err != nil {
			return err
		}
		price, err := a.createPrice(r.Context(), payload)
		if err != nil {
			return err
		}
		writeJSON(w, http.StatusCreated, price)
		return nil
	default:
		return methodNotAllowed()
	}
}

func (a *App) handleModelPriceByPath(w http.ResponseWriter, r *http.Request) error {
	if strings.TrimPrefix(r.URL.Path, "/api/model-prices/") == "sync/litellm" {
		if _, err := a.adminUser(r.Context(), r); err != nil {
			return err
		}
		if err := requireMethod(r, http.MethodPost); err != nil {
			return err
		}
		return a.handleSyncLiteLLMPrices(w, r)
	}
	if _, err := a.adminUser(r.Context(), r); err != nil {
		return err
	}
	idText := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/model-prices/"), "/")
	id, err := parseIntPath(idText)
	if err != nil {
		return err
	}
	switch r.Method {
	case http.MethodPut:
		var payload modelPricePayload
		if err := decodeJSON(r, &payload); err != nil {
			return err
		}
		price, err := a.updatePrice(r.Context(), id, payload)
		if err != nil {
			return err
		}
		writeJSON(w, http.StatusOK, price)
		return nil
	case http.MethodDelete:
		if err := a.deletePrice(r.Context(), id); err != nil {
			return err
		}
		writeNoContent(w)
		return nil
	default:
		return methodNotAllowed()
	}
}

func validatePricePayload(payload modelPricePayload) (modelPricePayload, error) {
	payload.Provider = strings.TrimSpace(payload.Provider)
	payload.Model = strings.TrimSpace(payload.Model)
	if payload.Provider == "" || payload.Model == "" {
		return payload, validationError("provider/model 不能为空")
	}
	if payload.InputUSDPerMillion < 0 || payload.OutputUSDPerMillion < 0 || payload.CachedUSDPerMillion < 0 || payload.ReasoningUSDPerMillion < 0 {
		return payload, validationError("价格不能为负数")
	}
	return payload, nil
}

func (a *App) listPrices(ctx context.Context) ([]ModelPrice, error) {
	rows, err := a.db.QueryContext(ctx, `
		SELECT id, provider, model, input_usd_per_million, output_usd_per_million,
		       cached_usd_per_million, reasoning_usd_per_million, source,
		       source_model, auto_synced, last_synced_at, updated_at
		FROM model_prices ORDER BY provider, model
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanPrices(rows)
}

func (a *App) priceMap(ctx context.Context) (map[[2]string]ModelPrice, error) {
	prices, err := a.listPrices(ctx)
	if err != nil {
		return nil, err
	}
	result := make(map[[2]string]ModelPrice, len(prices))
	for _, price := range prices {
		result[priceKey(price.Provider, price.Model)] = price
	}
	return result, nil
}

func scanPrices(rows *sql.Rows) ([]ModelPrice, error) {
	var prices []ModelPrice
	for rows.Next() {
		var price ModelPrice
		var sourceModel, lastSynced, updatedAt sql.NullString
		if err := rows.Scan(&price.ID, &price.Provider, &price.Model, &price.InputUSDPerMillion, &price.OutputUSDPerMillion, &price.CachedUSDPerMillion, &price.ReasoningUSDPerMillion, &price.Source, &sourceModel, &price.AutoSynced, &lastSynced, &updatedAt); err != nil {
			return nil, err
		}
		price.SourceModel = nullableString(sourceModel)
		price.LastSyncedAt = timePtr(lastSynced)
		if parsed, ok := parseDBTime(updatedAt.String); ok {
			price.UpdatedAt = parsed
		}
		prices = append(prices, price)
	}
	return prices, rows.Err()
}

func (a *App) createPrice(ctx context.Context, payload modelPricePayload) (ModelPrice, error) {
	payload, err := validatePricePayload(payload)
	if err != nil {
		return ModelPrice{}, err
	}
	now := dbTime(time.Now())
	result, err := a.db.ExecContext(ctx, `
		INSERT INTO model_prices (
			provider, model, input_usd_per_million, output_usd_per_million,
			cached_usd_per_million, reasoning_usd_per_million, source,
			source_model, auto_synced, last_synced_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, 'manual', NULL, 0, NULL, ?)
	`, payload.Provider, payload.Model, payload.InputUSDPerMillion, payload.OutputUSDPerMillion, payload.CachedUSDPerMillion, payload.ReasoningUSDPerMillion, now)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return ModelPrice{}, conflictError("该 provider/model 价格已存在")
		}
		return ModelPrice{}, err
	}
	id, _ := result.LastInsertId()
	return a.getPrice(ctx, int(id))
}

func (a *App) updatePrice(ctx context.Context, id int, payload modelPricePayload) (ModelPrice, error) {
	payload, err := validatePricePayload(payload)
	if err != nil {
		return ModelPrice{}, err
	}
	result, err := a.db.ExecContext(ctx, `
		UPDATE model_prices
		SET provider = ?, model = ?, input_usd_per_million = ?, output_usd_per_million = ?,
		    cached_usd_per_million = ?, reasoning_usd_per_million = ?, source = 'manual',
		    source_model = NULL, auto_synced = 0, last_synced_at = NULL, updated_at = ?
		WHERE id = ?
	`, payload.Provider, payload.Model, payload.InputUSDPerMillion, payload.OutputUSDPerMillion, payload.CachedUSDPerMillion, payload.ReasoningUSDPerMillion, dbTime(time.Now()), id)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return ModelPrice{}, conflictError("该 provider/model 价格已存在")
		}
		return ModelPrice{}, err
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return ModelPrice{}, notFoundError("模型价格不存在")
	}
	return a.getPrice(ctx, id)
}

func (a *App) deletePrice(ctx context.Context, id int) error {
	result, err := a.db.ExecContext(ctx, `DELETE FROM model_prices WHERE id = ?`, id)
	if err != nil {
		return err
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return notFoundError("模型价格不存在")
	}
	return nil
}

func (a *App) getPrice(ctx context.Context, id int) (ModelPrice, error) {
	rows, err := a.db.QueryContext(ctx, `
		SELECT id, provider, model, input_usd_per_million, output_usd_per_million,
		       cached_usd_per_million, reasoning_usd_per_million, source,
		       source_model, auto_synced, last_synced_at, updated_at
		FROM model_prices WHERE id = ?
	`, id)
	if err != nil {
		return ModelPrice{}, err
	}
	defer rows.Close()
	prices, err := scanPrices(rows)
	if err != nil {
		return ModelPrice{}, err
	}
	if len(prices) == 0 {
		return ModelPrice{}, notFoundError("模型价格不存在")
	}
	return prices[0], nil
}

func (a *App) handleSyncLiteLLMPrices(w http.ResponseWriter, r *http.Request) error {
	body := readAllAndRestore(r)
	var payload modelPriceSyncRequest
	if len(strings.TrimSpace(string(body))) > 0 {
		if err := decodeJSON(r, &payload); err != nil {
			return err
		}
	}
	sourceURL := defaultLiteLLMPricingURL
	if payload.SourceURL != nil && strings.TrimSpace(*payload.SourceURL) != "" {
		sourceURL = strings.TrimSpace(*payload.SourceURL)
	}
	if err := ensureHTTPSURL(sourceURL); err != nil {
		return err
	}
	response, rawPayload, err := doJSON(r.Context(), httpClient(30*time.Second), http.MethodGet, sourceURL, nil, nil)
	if err != nil {
		return validationError("下载 LiteLLM 价格数据失败")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return validationError(fmt.Sprintf("下载 LiteLLM 价格数据失败：HTTP %d", response.StatusCode))
	}
	var rawData map[string]any
	if err := json.Unmarshal(rawPayload, &rawData); err != nil {
		return validationError("LiteLLM 价格数据不是有效 JSON")
	}
	result, err := a.syncLiteLLMPrices(r.Context(), sourceURL, rawData)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, result)
	return nil
}

func (a *App) syncLiteLLMPrices(ctx context.Context, sourceURL string, rawData map[string]any) (map[string]any, error) {
	existing, err := a.priceMap(ctx)
	if err != nil {
		return nil, err
	}
	now := dbTime(time.Now())
	created, updated, unchanged, skippedManual, skippedInvalid := 0, 0, 0, 0, 0
	for modelName, rawEntry := range rawData {
		if modelName == "sample_spec" {
			skippedInvalid++
			continue
		}
		payload, ok := litellmEntryToPrice(modelName, rawEntry)
		if !ok {
			skippedInvalid++
			continue
		}
		key := priceKey(payload.Provider, payload.Model)
		item, exists := existing[key]
		if !exists {
			result, err := a.db.ExecContext(ctx, `
				INSERT INTO model_prices (
					provider, model, input_usd_per_million, output_usd_per_million,
					cached_usd_per_million, reasoning_usd_per_million, source,
					source_model, auto_synced, last_synced_at, updated_at
				) VALUES (?, ?, ?, ?, ?, ?, 'litellm', ?, 1, ?, ?)
			`, payload.Provider, payload.Model, payload.InputUSDPerMillion, payload.OutputUSDPerMillion, payload.CachedUSDPerMillion, payload.ReasoningUSDPerMillion, modelName, now, now)
			if err != nil {
				return nil, err
			}
			id, _ := result.LastInsertId()
			payloadPrice := ModelPrice{ID: int(id), Provider: payload.Provider, Model: payload.Model}
			existing[key] = payloadPrice
			created++
			continue
		}
		if !item.AutoSynced || item.Source != "litellm" {
			skippedManual++
			continue
		}
		if pricesEqual(item, payload) && item.SourceModel != nil && *item.SourceModel == modelName {
			unchanged++
			continue
		}
		_, err := a.db.ExecContext(ctx, `
			UPDATE model_prices
			SET provider = ?, model = ?, input_usd_per_million = ?, output_usd_per_million = ?,
			    cached_usd_per_million = ?, reasoning_usd_per_million = ?, source = 'litellm',
			    source_model = ?, auto_synced = 1, last_synced_at = ?, updated_at = ?
			WHERE id = ?
		`, payload.Provider, payload.Model, payload.InputUSDPerMillion, payload.OutputUSDPerMillion, payload.CachedUSDPerMillion, payload.ReasoningUSDPerMillion, modelName, now, now, item.ID)
		if err != nil {
			return nil, err
		}
		updated++
	}
	return map[string]any{
		"source_url":      sourceURL,
		"total_entries":   len(rawData),
		"imported":        created + updated,
		"created":         created,
		"updated":         updated,
		"unchanged":       unchanged,
		"skipped_manual":  skippedManual,
		"skipped_invalid": skippedInvalid,
	}, nil
}

func litellmEntryToPrice(modelName string, rawEntry any) (modelPricePayload, bool) {
	entry, ok := rawEntry.(map[string]any)
	if !ok {
		return modelPricePayload{}, false
	}
	provider := strings.ToLower(strings.TrimSpace(fmt.Sprint(entry["litellm_provider"])))
	model := strings.TrimSpace(modelName)
	if provider == "" || model == "" || len(provider) > 120 || len(model) > 180 {
		return modelPricePayload{}, false
	}
	payload := modelPricePayload{
		Provider:               provider,
		Model:                  model,
		InputUSDPerMillion:     usdPerMillion(entry["input_cost_per_token"]),
		OutputUSDPerMillion:    usdPerMillion(entry["output_cost_per_token"]),
		CachedUSDPerMillion:    usdPerMillion(entry["cache_read_input_token_cost"]),
		ReasoningUSDPerMillion: 0,
	}
	if payload.InputUSDPerMillion == 0 && payload.OutputUSDPerMillion == 0 && payload.CachedUSDPerMillion == 0 && payload.ReasoningUSDPerMillion == 0 {
		return modelPricePayload{}, false
	}
	return payload, true
}

func usdPerMillion(value any) float64 {
	number, ok := numeric(value)
	if !ok || number < 0 {
		return 0
	}
	return mathRound(number*1_000_000, 12)
}

func numeric(value any) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, true
	case int:
		return float64(typed), true
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		return parsed, err == nil
	default:
		return 0, false
	}
}

func mathRound(value float64, places int) float64 {
	factor := 1.0
	for i := 0; i < places; i++ {
		factor *= 10
	}
	if value >= 0 {
		return float64(int64(value*factor+0.5)) / factor
	}
	return float64(int64(value*factor-0.5)) / factor
}

func pricesEqual(item ModelPrice, payload modelPricePayload) bool {
	return item.Provider == payload.Provider &&
		item.Model == payload.Model &&
		item.InputUSDPerMillion == payload.InputUSDPerMillion &&
		item.OutputUSDPerMillion == payload.OutputUSDPerMillion &&
		item.CachedUSDPerMillion == payload.CachedUSDPerMillion &&
		item.ReasoningUSDPerMillion == payload.ReasoningUSDPerMillion
}

func priceKey(provider, model string) [2]string {
	return [2]string{strings.ToLower(strings.TrimSpace(provider)), strings.ToLower(strings.TrimSpace(model))}
}

func findMatchingPrice(prices map[[2]string]ModelPrice, provider, model *string) *ModelPrice {
	if provider == nil || model == nil {
		return nil
	}
	providerKey := strings.ToLower(strings.TrimSpace(*provider))
	modelKey := strings.ToLower(strings.TrimSpace(*model))
	if providerKey == "" || modelKey == "" {
		return nil
	}
	candidates := []string{providerKey}
	if providerKey == "codex" {
		candidates = append(candidates, "openai")
	}
	if providerKey == "claude" {
		candidates = append(candidates, "anthropic")
	}
	for _, candidate := range candidates {
		if price, ok := prices[[2]string{candidate, modelKey}]; ok {
			return &price
		}
	}
	return nil
}

func recordCost(record UsageRecord, prices map[[2]string]ModelPrice) (float64, bool) {
	price := findMatchingPrice(prices, record.Provider, record.Model)
	if price == nil {
		return 0, record.TotalTokens > 0
	}
	inputTokens, cachedTokens := splitPricedSubset(record.InputTokens, record.CachedTokens, price.CachedUSDPerMillion)
	outputTokens, reasoningTokens := splitPricedSubset(record.OutputTokens, record.ReasoningTokens, price.ReasoningUSDPerMillion)
	amount := millionTokenCost(inputTokens, price.InputUSDPerMillion) +
		millionTokenCost(outputTokens, price.OutputUSDPerMillion) +
		millionTokenCost(cachedTokens, price.CachedUSDPerMillion) +
		millionTokenCost(reasoningTokens, price.ReasoningUSDPerMillion)
	return mathRound(amount, 8), false
}

func splitPricedSubset(totalTokens, subsetTokens int, subsetPrice float64) (int, int) {
	if totalTokens < 0 {
		totalTokens = 0
	}
	if subsetTokens < 0 {
		subsetTokens = 0
	}
	if subsetTokens > totalTokens {
		subsetTokens = totalTokens
	}
	if subsetPrice <= 0 {
		return totalTokens, 0
	}
	return totalTokens - subsetTokens, subsetTokens
}

func millionTokenCost(tokens int, usdPerMillion float64) float64 {
	return float64(tokens) / 1_000_000 * usdPerMillion
}

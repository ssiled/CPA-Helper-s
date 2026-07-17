package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	defaultCardShopsURL             = "https://ldxp.qizhang.org/api/shops"
	defaultCardShopGoodsInfoBaseURL = "https://pay.ldxp.cn"
	cardShopsTimeout                = 10 * time.Second
	cardShopProductStatusTimeout    = 8 * time.Second
	maxCardShopTags                 = 30
	maxCardShopTagRuneCount         = 32
	maxCardShopGoodsKeyLength       = 128
)

var cardShopGoodsKeyPattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

type cardShopsResponse struct {
	Shops     []map[string]any `json:"shops"`
	FetchedAt string           `json:"fetched_at"`
}

type cardShopFavoritesResponse struct {
	ShopKeys []string `json:"shop_keys"`
}

type cardShopFavoriteUpdateRequest struct {
	ShopKey  string `json:"shop_key"`
	Favorite bool   `json:"favorite"`
}

type cardShopTagsResponse struct {
	Tags []string `json:"tags"`
}

type cardShopTagsUpdateRequest struct {
	Tags []string `json:"tags"`
}

type cardShopProductStatusRequest struct {
	ItemURL  string `json:"item_url"`
	GoodsKey string `json:"goods_key"`
}

type cardShopProductStatusResponse struct {
	GoodsKey   string `json:"goods_key"`
	Available  bool   `json:"available"`
	Status     string `json:"status"`
	Message    string `json:"message"`
	StockCount *int   `json:"stock_count"`
	CheckedAt  string `json:"checked_at"`
}

func (a *App) handleCardShops(w http.ResponseWriter, r *http.Request) error {
	if err := requireMethod(r, http.MethodGet); err != nil {
		return err
	}
	if _, err := a.readyUser(r.Context(), r); err != nil {
		return err
	}

	shops, err := fetchCardShops(r.Context())
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, cardShopsResponse{
		Shops:     shops,
		FetchedAt: apiDateTime(time.Now()),
	})
	return nil
}

func (a *App) handleCardShopProductStatus(w http.ResponseWriter, r *http.Request) error {
	if err := requireMethod(r, http.MethodPost); err != nil {
		return err
	}
	if _, err := a.readyUser(r.Context(), r); err != nil {
		return err
	}

	var payload cardShopProductStatusRequest
	if err := decodeJSON(r, &payload); err != nil {
		return err
	}
	goodsKey, err := normalizeCardShopGoodsKey(payload)
	if err != nil {
		return err
	}

	status, err := fetchCardShopProductStatus(r.Context(), goodsKey)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, status)
	return nil
}

func (a *App) handleCardShopFavorites(w http.ResponseWriter, r *http.Request) error {
	user, err := a.readyUser(r.Context(), r)
	if err != nil {
		return err
	}

	switch r.Method {
	case http.MethodGet:
		keys, err := a.cardShopFavoriteKeys(r.Context(), user.ID)
		if err != nil {
			return err
		}
		writeJSON(w, http.StatusOK, cardShopFavoritesResponse{ShopKeys: keys})
		return nil
	case http.MethodPut:
		var payload cardShopFavoriteUpdateRequest
		if err := decodeJSON(r, &payload); err != nil {
			return err
		}
		shopKey, err := normalizeCardShopFavoriteKey(payload.ShopKey)
		if err != nil {
			return err
		}
		if err := a.setCardShopFavorite(r.Context(), user.ID, shopKey, payload.Favorite); err != nil {
			return err
		}
		keys, err := a.cardShopFavoriteKeys(r.Context(), user.ID)
		if err != nil {
			return err
		}
		writeJSON(w, http.StatusOK, cardShopFavoritesResponse{ShopKeys: keys})
		return nil
	default:
		return methodNotAllowed()
	}
}

func (a *App) handleCardShopTags(w http.ResponseWriter, r *http.Request) error {
	user, err := a.readyUser(r.Context(), r)
	if err != nil {
		return err
	}

	switch r.Method {
	case http.MethodGet:
		tags, err := a.cardShopTags(r.Context(), user.ID)
		if err != nil {
			return err
		}
		writeJSON(w, http.StatusOK, cardShopTagsResponse{Tags: tags})
		return nil
	case http.MethodPut:
		var payload cardShopTagsUpdateRequest
		if err := decodeJSON(r, &payload); err != nil {
			return err
		}
		tags, err := normalizeCardShopTags(payload.Tags)
		if err != nil {
			return err
		}
		if err := a.replaceCardShopTags(r.Context(), user.ID, tags); err != nil {
			return err
		}
		writeJSON(w, http.StatusOK, cardShopTagsResponse{Tags: tags})
		return nil
	default:
		return methodNotAllowed()
	}
}

func fetchCardShops(ctx context.Context) ([]map[string]any, error) {
	headers := http.Header{}
	headers.Set("Accept", "application/json")
	headers.Set("User-Agent", "CPA-Helper/1.0 (+https://github.com/ssiled/CPA-Helper-s)")
	response, payload, err := doJSON(ctx, httpClient(cardShopsTimeout), http.MethodGet, cardShopsSourceURL(), headers, nil)
	if err != nil {
		return nil, appError("upstream_error", http.StatusBadGateway, "卡网收录数据源暂时不可用")
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, appError("upstream_error", http.StatusBadGateway, "卡网收录数据源暂时不可用")
	}

	var raw struct {
		Shops json.RawMessage `json:"shops"`
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.UseNumber()
	if err := decoder.Decode(&raw); err != nil {
		return nil, appError("upstream_error", http.StatusBadGateway, "卡网收录数据格式无效")
	}
	if len(raw.Shops) == 0 {
		return nil, appError("upstream_error", http.StatusBadGateway, "卡网收录数据格式无效")
	}
	var shops []map[string]any
	shopDecoder := json.NewDecoder(bytes.NewReader(raw.Shops))
	shopDecoder.UseNumber()
	if err := shopDecoder.Decode(&shops); err != nil {
		return nil, appError("upstream_error", http.StatusBadGateway, "卡网收录数据格式无效")
	}
	if shops == nil {
		shops = []map[string]any{}
	}
	normalizeCardShopProductItems(shops)
	return shops, nil
}

func normalizeCardShopProductItems(shops []map[string]any) {
	for _, shop := range shops {
		if len(anySlice(shop["productItems"])) > 0 {
			continue
		}

		items := cardShopPreviewItems(shop)
		if len(items) == 0 {
			continue
		}
		shop["productItems"] = items
	}
}

func cardShopPreviewItems(shop map[string]any) []map[string]any {
	summary, ok := shop["productSummary"].(map[string]any)
	if !ok {
		return nil
	}

	items := []map[string]any{}
	for _, rawGroup := range anySlice(summary["groups"]) {
		group, ok := rawGroup.(map[string]any)
		if !ok {
			continue
		}
		groupName, _ := group["group"].(string)
		for _, rawItem := range anySlice(group["previewItems"]) {
			item, ok := rawItem.(map[string]any)
			if !ok {
				continue
			}
			product := make(map[string]any, len(item)+1)
			for key, value := range item {
				product[key] = value
			}
			if _, exists := product["group"]; !exists && strings.TrimSpace(groupName) != "" {
				product["group"] = groupName
			}
			items = append(items, product)
		}
	}
	return items
}

func anySlice(value any) []any {
	items, _ := value.([]any)
	return items
}

func cardShopsSourceURL() string {
	if value := strings.TrimSpace(os.Getenv("CPA_HELPER_CARD_SHOPS_URL")); value != "" {
		return value
	}
	return defaultCardShopsURL
}

func cardShopGoodsInfoBaseURL() string {
	if value := strings.TrimSpace(os.Getenv("CPA_HELPER_CARD_SHOP_GOODS_INFO_BASE_URL")); value != "" {
		return value
	}
	return defaultCardShopGoodsInfoBaseURL
}

func normalizeCardShopGoodsKey(payload cardShopProductStatusRequest) (string, error) {
	if key := strings.TrimSpace(payload.GoodsKey); key != "" {
		return validateCardShopGoodsKey(key)
	}

	itemURL := strings.TrimSpace(payload.ItemURL)
	if itemURL == "" {
		return "", validationError("商品链接不能为空")
	}
	parsed, err := url.Parse(itemURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", validationError("商品链接无效")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", validationError("商品链接必须是 HTTP/HTTPS 地址")
	}
	if !strings.EqualFold(parsed.Hostname(), "pay.ldxp.cn") {
		return "", validationError("仅支持 pay.ldxp.cn 商品链接")
	}
	parts := strings.Split(strings.Trim(parsed.EscapedPath(), "/"), "/")
	if len(parts) != 2 || parts[0] != "item" {
		return "", validationError("商品链接必须是 pay.ldxp.cn/item/<key> 格式")
	}
	goodsKey, err := url.PathUnescape(parts[1])
	if err != nil {
		return "", validationError("商品链接无效")
	}
	return validateCardShopGoodsKey(goodsKey)
}

func validateCardShopGoodsKey(value string) (string, error) {
	key := strings.TrimSpace(value)
	if key == "" {
		return "", validationError("商品标识不能为空")
	}
	if len(key) > maxCardShopGoodsKeyLength || !cardShopGoodsKeyPattern.MatchString(key) {
		return "", validationError("商品标识无效")
	}
	return key, nil
}

func fetchCardShopProductStatus(ctx context.Context, goodsKey string) (cardShopProductStatusResponse, error) {
	headers := http.Header{}
	headers.Set("Accept", "application/json")
	headers.Set("Content-Type", "application/json")
	headers.Set("User-Agent", "CPA-Helper/1.0 (+https://github.com/ssiled/CPA-Helper-s)")
	headers.Set("Origin", "https://pay.ldxp.cn")
	headers.Set("Referer", "https://pay.ldxp.cn/item/"+goodsKey)

	response, payload, err := doJSON(ctx, httpClient(cardShopProductStatusTimeout), http.MethodPost, makeURL(cardShopGoodsInfoBaseURL(), "/shopApi/Shop/goodsInfo", nil), headers, map[string]string{
		"goods_key": goodsKey,
	})
	if err != nil {
		return cardShopProductStatusResponse{}, appError("upstream_error", http.StatusBadGateway, "商品实时状态查询失败")
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return cardShopProductStatusResponse{}, appError("upstream_error", http.StatusBadGateway, fmt.Sprintf("商品实时状态查询失败：HTTP %d", response.StatusCode))
	}
	return parseCardShopProductStatus(goodsKey, payload)
}

func parseCardShopProductStatus(goodsKey string, payload []byte) (cardShopProductStatusResponse, error) {
	var raw struct {
		Code json.Number     `json:"code"`
		Msg  string          `json:"msg"`
		Data json.RawMessage `json:"data"`
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.UseNumber()
	if err := decoder.Decode(&raw); err != nil {
		return cardShopProductStatusResponse{}, appError("upstream_error", http.StatusBadGateway, "商品实时状态响应格式无效")
	}

	checkedAt := apiDateTime(time.Now())
	message := strings.TrimSpace(raw.Msg)
	if raw.Code.String() != "1" {
		if message == "" {
			message = "商品不可下单"
		}
		status := "unavailable"
		if strings.Contains(message, "不存在") {
			status = "not_found"
		}
		return cardShopProductStatusResponse{GoodsKey: goodsKey, Available: false, Status: status, Message: message, CheckedAt: checkedAt}, nil
	}

	var data map[string]any
	if len(raw.Data) > 0 && string(raw.Data) != "null" {
		dataDecoder := json.NewDecoder(bytes.NewReader(raw.Data))
		dataDecoder.UseNumber()
		if err := dataDecoder.Decode(&data); err != nil {
			return cardShopProductStatusResponse{}, appError("upstream_error", http.StatusBadGateway, "商品实时状态响应格式无效")
		}
	}
	if data == nil {
		return cardShopProductStatusResponse{GoodsKey: goodsKey, Available: false, Status: "unavailable", Message: "商品不可下单", CheckedAt: checkedAt}, nil
	}

	if status := optionalInt(data["status"]); status != nil && *status != 1 {
		return cardShopProductStatusResponse{GoodsKey: goodsKey, Available: false, Status: "disabled", Message: "商品已下架", CheckedAt: checkedAt}, nil
	}
	if verify := optionalInt(data["verify"]); verify != nil && *verify != 1 {
		return cardShopProductStatusResponse{GoodsKey: goodsKey, Available: false, Status: "unverified", Message: "商品未通过校验", CheckedAt: checkedAt}, nil
	}

	stockCount := cardShopStockCount(data)
	if stockCount != nil && *stockCount <= 0 {
		return cardShopProductStatusResponse{GoodsKey: goodsKey, Available: false, Status: "out_of_stock", Message: "商品缺货", StockCount: stockCount, CheckedAt: checkedAt}, nil
	}
	return cardShopProductStatusResponse{GoodsKey: goodsKey, Available: true, Status: "available", Message: "商品可下单", StockCount: stockCount, CheckedAt: checkedAt}, nil
}

func cardShopStockCount(data map[string]any) *int {
	if value := optionalInt(data["stock_count"]); value != nil {
		return value
	}
	if extend, ok := data["extend"].(map[string]any); ok {
		return optionalInt(extend["stock_count"])
	}
	return nil
}

func optionalInt(value any) *int {
	switch typed := value.(type) {
	case nil:
		return nil
	case json.Number:
		if integer, err := typed.Int64(); err == nil {
			result := int(integer)
			return &result
		}
		if float, err := strconv.ParseFloat(typed.String(), 64); err == nil {
			result := int(float)
			return &result
		}
	case float64:
		result := int(typed)
		return &result
	case int:
		result := typed
		return &result
	case string:
		trimmed := strings.TrimSpace(typed)
		if trimmed == "" {
			return nil
		}
		if integer, err := strconv.Atoi(trimmed); err == nil {
			return &integer
		}
	}
	return nil
}

func normalizeCardShopFavoriteKey(value string) (string, error) {
	key := strings.TrimSpace(value)
	if key == "" {
		return "", validationError("店铺标识不能为空")
	}
	if len([]rune(key)) > 1024 {
		return "", validationError("店铺标识过长")
	}
	return key, nil
}

func normalizeCardShopTags(values []string) ([]string, error) {
	if len(values) > maxCardShopTags {
		return nil, validationError("快速搜索标签不能超过 30 个")
	}

	tags := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		tag := strings.TrimSpace(value)
		if tag == "" {
			return nil, validationError("快速搜索标签不能为空")
		}
		if len([]rune(tag)) > maxCardShopTagRuneCount {
			return nil, validationError("快速搜索标签不能超过 32 个字符")
		}
		key := strings.ToLower(tag)
		if _, exists := seen[key]; exists {
			return nil, validationError("快速搜索标签不能重复")
		}
		seen[key] = struct{}{}
		tags = append(tags, tag)
	}
	return tags, nil
}

func (a *App) cardShopFavoriteKeys(ctx context.Context, userID int) ([]string, error) {
	rows, err := a.db.QueryContext(ctx, `
		SELECT shop_key
		FROM user_card_shop_favorites
		WHERE user_id = ?
		ORDER BY created_at ASC, shop_key ASC
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	keys := []string{}
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return nil, err
		}
		keys = append(keys, key)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.Strings(keys)
	return keys, nil
}

func (a *App) setCardShopFavorite(ctx context.Context, userID int, shopKey string, favorite bool) error {
	if favorite {
		now := dbTime(time.Now())
		_, err := a.db.ExecContext(ctx, `
			INSERT INTO user_card_shop_favorites (user_id, shop_key, created_at, updated_at)
			VALUES (?, ?, ?, ?)
			ON CONFLICT(user_id, shop_key) DO UPDATE SET updated_at = excluded.updated_at
		`, userID, shopKey, now, now)
		return err
	}

	_, err := a.db.ExecContext(ctx, `
		DELETE FROM user_card_shop_favorites
		WHERE user_id = ? AND shop_key = ?
	`, userID, shopKey)
	return err
}

func (a *App) cardShopTags(ctx context.Context, userID int) ([]string, error) {
	rows, err := a.db.QueryContext(ctx, `
		SELECT tag
		FROM user_card_shop_tags
		WHERE user_id = ?
		ORDER BY position ASC, tag ASC
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	tags := []string{}
	for rows.Next() {
		var tag string
		if err := rows.Scan(&tag); err != nil {
			return nil, err
		}
		tags = append(tags, tag)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return tags, nil
}

func (a *App) replaceCardShopTags(ctx context.Context, userID int, tags []string) error {
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM user_card_shop_tags WHERE user_id = ?`, userID); err != nil {
		return err
	}

	now := dbTime(time.Now())
	for index, tag := range tags {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO user_card_shop_tags (user_id, tag, position, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?)
		`, userID, tag, index, now, now); err != nil {
			return err
		}
	}

	return tx.Commit()
}

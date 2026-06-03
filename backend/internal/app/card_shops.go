package app

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"
)

const (
	defaultCardShopsURL = "https://ldxp.qizhang.org/api/shops"
	cardShopsTimeout    = 10 * time.Second
)

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

func (a *App) handleCardShops(w http.ResponseWriter, r *http.Request) error {
	if err := requireMethod(r, http.MethodGet); err != nil {
		return err
	}
	if _, err := a.adminUser(r.Context(), r); err != nil {
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

func fetchCardShops(ctx context.Context) ([]map[string]any, error) {
	headers := http.Header{}
	headers.Set("Accept", "application/json")
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
	return shops, nil
}

func cardShopsSourceURL() string {
	if value := strings.TrimSpace(os.Getenv("CPA_HELPER_CARD_SHOPS_URL")); value != "" {
		return value
	}
	return defaultCardShopsURL
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

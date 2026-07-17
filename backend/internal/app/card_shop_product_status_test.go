package app_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	backendApp "cpa-helper/backend/internal/app"
)

func TestCardShopProductStatusChecksRealtimeGoodsInfo(t *testing.T) {
	tests := []struct {
		name       string
		goodsKey   string
		upstream   map[string]any
		available  bool
		status     string
		message    string
		stockCount *int
	}{
		{
			name:      "not found",
			goodsKey:  "r4hqtz",
			upstream:  map[string]any{"code": 0, "msg": "商品不存在", "data": nil},
			available: false,
			status:    "not_found",
			message:   "商品不存在",
		},
		{
			name:     "available",
			goodsKey: "mcq58m",
			upstream: map[string]any{
				"code": 1,
				"msg":  "ok",
				"data": map[string]any{"status": 1, "verify": 1, "extend": map[string]any{"stock_count": 5}},
			},
			available:  true,
			status:     "available",
			message:    "商品可下单",
			stockCount: cardShopIntPtr(5),
		},
		{
			name:     "out of stock",
			goodsKey: "empty1",
			upstream: map[string]any{
				"code": 1,
				"data": map[string]any{"status": 1, "verify": 1, "extend": map[string]any{"stock_count": 0}},
			},
			available:  false,
			status:     "out_of_stock",
			message:    "商品缺货",
			stockCount: cardShopIntPtr(0),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("CPA_HELPER_DATA_DIR", t.TempDir())
			upstreamCalls := 0
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				upstreamCalls++
				if r.Method != http.MethodPost || r.URL.Path != "/shopApi/Shop/goodsInfo" {
					t.Fatalf("upstream request = %s %s, want POST /shopApi/Shop/goodsInfo", r.Method, r.URL.Path)
				}
				if got := r.Header.Get("Referer"); got == "" {
					t.Fatal("Referer is empty")
				}
				var request map[string]string
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Fatalf("decode request failed: %v", err)
				}
				if request["goods_key"] != tt.goodsKey {
					t.Fatalf("goods_key = %q, want %q", request["goods_key"], tt.goodsKey)
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(tt.upstream)
			}))
			defer upstream.Close()
			t.Setenv("CPA_HELPER_CARD_SHOP_GOODS_INFO_BASE_URL", upstream.URL)

			app, err := backendApp.New()
			if err != nil {
				t.Fatalf("New() failed: %v", err)
			}
			defer app.Close()

			handler := app.Routes()
			cookies := requestJSON(t, handler, http.MethodPost, "/api/auth/setup", map[string]any{
				"username": "admin",
				"password": "password123",
				"nickname": "Admin",
			}, nil, nil)

			var response struct {
				GoodsKey   string `json:"goods_key"`
				Available  bool   `json:"available"`
				Status     string `json:"status"`
				Message    string `json:"message"`
				StockCount *int   `json:"stock_count"`
				CheckedAt  string `json:"checked_at"`
			}
			requestJSON(t, handler, http.MethodPost, "/api/card-shops/product-status", map[string]any{
				"item_url": "https://pay.ldxp.cn/item/" + tt.goodsKey,
			}, cookies, &response)

			if upstreamCalls != 1 {
				t.Fatalf("upstream calls = %d, want 1", upstreamCalls)
			}
			if response.GoodsKey != tt.goodsKey || response.Available != tt.available || response.Status != tt.status || response.Message != tt.message || response.CheckedAt == "" {
				t.Fatalf("response = %#v, want goods=%s available=%v status=%s message=%s", response, tt.goodsKey, tt.available, tt.status, tt.message)
			}
			if (response.StockCount == nil) != (tt.stockCount == nil) || (response.StockCount != nil && *response.StockCount != *tt.stockCount) {
				t.Fatalf("stock_count = %#v, want %#v", response.StockCount, tt.stockCount)
			}
		})
	}
}

func TestCardShopProductStatusRejectsUnsupportedItemURL(t *testing.T) {
	t.Setenv("CPA_HELPER_DATA_DIR", t.TempDir())
	upstreamCalls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalls++
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()
	t.Setenv("CPA_HELPER_CARD_SHOP_GOODS_INFO_BASE_URL", upstream.URL)

	app, err := backendApp.New()
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	defer app.Close()

	handler := app.Routes()
	cookies := requestJSON(t, handler, http.MethodPost, "/api/auth/setup", map[string]any{
		"username": "admin",
		"password": "password123",
		"nickname": "Admin",
	}, nil, nil)

	requestJSONExpectStatus(t, handler, http.MethodPost, "/api/card-shops/product-status", map[string]any{
		"item_url": "https://example.com/item/r4hqtz",
	}, cookies, http.StatusUnprocessableEntity)
	if upstreamCalls != 0 {
		t.Fatalf("upstream calls = %d, want 0", upstreamCalls)
	}
}

func cardShopIntPtr(value int) *int {
	return &value
}

-- +goose Up
CREATE TABLE IF NOT EXISTS user_card_shop_favorites (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	user_id INTEGER NOT NULL,
	shop_key VARCHAR(1024) NOT NULL,
	created_at DATETIME NOT NULL,
	updated_at DATETIME NOT NULL,
	FOREIGN KEY(user_id) REFERENCES users(id),
	CONSTRAINT uq_user_card_shop_favorites_user_shop UNIQUE (user_id, shop_key)
);

CREATE INDEX IF NOT EXISTS ix_user_card_shop_favorites_user_id
	ON user_card_shop_favorites(user_id);

CREATE INDEX IF NOT EXISTS ix_user_card_shop_favorites_created_at
	ON user_card_shop_favorites(created_at);

-- +goose Down
DROP TABLE IF EXISTS user_card_shop_favorites;

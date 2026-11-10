-- v0.1.3 chat cards (data-model.md 3.12, api.md 2 "카드"): kind "card" is stored as kind 'text' + card_json.
-- The kind CHECK is not rebuilt: recreating messages would delete child rows through ON DELETE CASCADE.
ALTER TABLE messages ADD COLUMN card_json TEXT; -- JSON {type, v, data}; NULL = not a card

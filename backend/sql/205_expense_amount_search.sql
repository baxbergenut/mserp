-- Exact amount searches retain text matches through a separate indexed ID lookup.
-- Also supports the existing amount-range filters. No invoice values are changed.
CREATE INDEX IF NOT EXISTS expenses_amount_category_idx
    ON expenses (amount, category_id) INCLUDE (id);

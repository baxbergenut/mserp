-- Unknown colors stay unknown until entered or confirmed through reconciliation.
ALTER TABLE trucks ADD COLUMN color TEXT
    CONSTRAINT trucks_color_length CHECK (color IS NULL OR char_length(color) BETWEEN 1 AND 30);

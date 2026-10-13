BEGIN;

ALTER TABLE gross_board_entries ADD COLUMN note text NOT NULL DEFAULT '' CHECK (char_length(note) <= 5000);
ALTER TABLE gross_board_extra_entries ADD COLUMN note text NOT NULL DEFAULT '' CHECK (char_length(note) <= 5000);

COMMIT;

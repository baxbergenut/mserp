BEGIN;
-- Existing display names are preserved; never guess name parts.
ALTER TABLE app_users
 ADD COLUMN first_name text NOT NULL DEFAULT '' CHECK (length(first_name)<=50),
 ADD COLUMN last_name text NOT NULL DEFAULT '' CHECK (length(last_name)<=50),
 ADD COLUMN date_of_birth date CHECK (date_of_birth >= DATE '1900-01-01' AND isfinite(date_of_birth)),
 ADD COLUMN phone text NOT NULL DEFAULT '' CHECK (length(phone)<=40),
 ADD COLUMN birthday_greeted_year integer;
COMMIT;

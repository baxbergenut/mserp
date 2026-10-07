BEGIN;

UPDATE expense_settings
SET name='Escrow', version=version+1
WHERE kind='name' AND lower(btrim(name))='escrow payment';

UPDATE expenses
SET expense_type='Escrow', updated_at=now()
WHERE system_kind='driver_escrow' AND expense_type IS DISTINCT FROM 'Escrow';

COMMIT;

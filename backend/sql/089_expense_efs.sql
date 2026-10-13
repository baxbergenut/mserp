BEGIN;

-- Preserve historical totals; fee-aware writes store the entered principal separately.
ALTER TABLE expenses ADD COLUMN base_amount NUMERIC(14,2);
ALTER TABLE expenses ADD COLUMN payment_fee NUMERIC(14,2) NOT NULL DEFAULT 0;
ALTER TABLE expenses ADD CONSTRAINT expense_payment_fee CHECK (
    payment_fee >= 0 AND (base_amount IS NULL OR amount = base_amount + payment_fee)
);
INSERT INTO expense_settings(kind,name) VALUES('payment_method','EFS') ON CONFLICT DO NOTHING;
UPDATE expense_settings SET name='EFS', active=true, version=version+1 WHERE kind='payment_method' AND lower(name)='efs';

CREATE FUNCTION protect_efs_payment_method() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF OLD.kind='payment_method' AND lower(OLD.name)='efs' THEN
        IF TG_OP='DELETE' THEN
            RAISE EXCEPTION 'EFS is a permanent payment method' USING ERRCODE='23514';
        END IF;
        IF NEW.kind<>OLD.kind OR NEW.name<>'EFS' OR NOT NEW.active THEN
            RAISE EXCEPTION 'EFS is a permanent payment method' USING ERRCODE='23514';
        END IF;
    END IF;
    IF TG_OP='DELETE' THEN RETURN OLD; END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER expense_settings_protect_efs BEFORE UPDATE OR DELETE ON expense_settings
FOR EACH ROW EXECUTE FUNCTION protect_efs_payment_method();

COMMIT;

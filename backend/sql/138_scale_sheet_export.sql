BEGIN;

-- Separate opt-in: deploying must not copy old or newly imported scale tickets.
ALTER TABLE expense_sheet_export_config
 ADD COLUMN scale_enabled boolean NOT NULL DEFAULT false,
 ADD COLUMN scale_from_date date;
ALTER TABLE expense_sheet_export_config ADD CONSTRAINT scale_sheet_start_required
 CHECK (NOT scale_enabled OR scale_from_date IS NOT NULL);

CREATE FUNCTION capture_scale_sheet_export() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE c expense_sheet_export_config; t weighmytruck_transactions;
 owner_record record; payer text; driver_name text; export_id uuid; snapshot jsonb;
BEGIN
 SELECT * INTO c FROM expense_sheet_export_config WHERE singleton;
 IF NOT FOUND OR NOT c.enabled OR NOT c.scale_enabled THEN RETURN NEW; END IF;
 -- Deferred until the import has resolved driver/truck IDs and warnings.
 SELECT * INTO t FROM weighmytruck_transactions WHERE id=NEW.id;
 IF NOT FOUND OR t.transaction_date<c.scale_from_date THEN RETURN NEW; END IF;
 SELECT i.id,i.is_company,i.driver_id INTO owner_record FROM investors i
 JOIN (SELECT (array_agg(DISTINCT h.owner_id))[1] owner_id FROM truck_ownership_history h
  WHERE h.truck_id=t.truck_id
   AND (NOT h.start_known OR (h.assigned_at AT TIME ZONE 'America/New_York')::date<=t.transaction_date)
   AND (h.unassigned_at IS NULL OR t.transaction_date<(h.unassigned_at AT TIME ZONE 'America/New_York')::date)
  HAVING count(DISTINCT h.owner_id)=1) o ON o.owner_id=i.id;
 payer:=CASE WHEN t.driver_id IS NULL OR owner_record.id IS NULL OR t.match_warning LIKE '%conflicts%' THEN 'Needs review'
  WHEN owner_record.is_company THEN 'Company'
  WHEN owner_record.driver_id=payroll_driver_at(t.driver_id,t.transaction_date) THEN 'Driver'
  ELSE 'Truck Owner' END;
 SELECT full_name INTO driver_name FROM drivers WHERE id=t.driver_id;
 -- Namespaced UUID works with the existing atomic sheet receipts/import exclusion.
 export_id:=md5('weighmytruck:'||t.site_number||':'||t.ticket_number)::uuid;
 snapshot:=jsonb_build_object(
  'id',export_id,'category','Other','expense_type','Scale',
  'expense_date',t.transaction_date,'amount',t.amount,'unit_number',t.tractor_number,
  'driver_name',coalesce(driver_name,''),'payment_type','WeighMyTruck',
  'reference_number',t.site_number||'/'||t.ticket_number,
  'description',concat_ws(' · ','CAT Scale',nullif(t.location,''),nullif(t.city,''),nullif(t.state,''),
   CASE WHEN payer='Needs review' THEN 'Payer needs review' END,nullif(t.match_warning,'')),
  'covered_by',payer,'paid_by','','manager_verified',false,'accounting_verified',false,
  'driver_settled',false,'created_at',now());
 INSERT INTO expense_sheet_exports(expense_id,spreadsheet_id,sheet_id,payload)
 VALUES(export_id,c.spreadsheet_id,c.maintenance_sheet_id,
  jsonb_build_object('expense',snapshot,'source','weighmytruck','scale_ticket',to_jsonb(t)))
 ON CONFLICT(expense_id) DO NOTHING;
 RETURN NEW;
END $$;
CREATE CONSTRAINT TRIGGER scale_sheet_export_capture AFTER INSERT ON weighmytruck_transactions
 DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION capture_scale_sheet_export();

COMMIT;

BEGIN;
-- Capability cutover: sensitive profile and document access requires explicit review.
CREATE TEMP TABLE permission_before ON COMMIT DROP AS SELECT r.id,r.name,r.permissions,coalesce((SELECT jsonb_agg(to_jsonb(a) ORDER BY a.category_id) FROM role_expense_category_access a WHERE a.role_id=r.id),'[]'::jsonb) AS categories FROM app_roles r WHERE NOT system_role;
UPDATE app_roles r SET permissions=ARRAY(SELECT DISTINCT expanded FROM unnest(r.permissions) old CROSS JOIN LATERAL unnest(CASE old
 WHEN 'fuel.read' THEN ARRAY['fuel.read','fuel.drivers.sync']::text[]
 WHEN 'tolls.read' THEN ARRAY['tolls.read','tolls.fleet.sync']::text[]
 WHEN 'fleet.read' THEN ARRAY['drivers.read','trucks.read','dispatchers.read','updaters.read','investors.read']::text[]
 WHEN 'fleet.write' THEN ARRAY['drivers.create','drivers.edit','drivers.delete','trucks.create','trucks.edit','trucks.delete','dispatchers.create','dispatchers.edit','dispatchers.delete','updaters.create','updaters.edit','updaters.delete','investors.create','investors.edit']::text[]
 WHEN 'payroll.read' THEN ARRAY['driver_pay.read','investor_pay.read','dispatcher_pay.read','driver_compensation.read','dispatcher_compensation.read']::text[]
 WHEN 'payroll.write' THEN ARRAY['driver_pay.edit','investor_pay.edit','driver_compensation.edit','dispatcher_compensation.edit']::text[]
 WHEN 'payroll.finalize' THEN ARRAY['driver_pay.finalize','driver_pay.reopen','investor_pay.finalize','investor_pay.reopen']::text[]
 WHEN 'charges.read' THEN ARRAY['charges.read','investor_terms.read']::text[]
 WHEN 'charges.write' THEN ARRAY['charges.create','charges.edit','charges.delete','investor_terms.edit']::text[]
 WHEN 'escrow.write' THEN ARRAY['escrow.create','escrow.edit','escrow.delete']::text[]
 WHEN 'tasks.write' THEN ARRAY['tasks.create','tasks.edit','tasks.delete','tasks.review']::text[]
 WHEN 'fuel.drivers.write' THEN ARRAY['fuel.drivers.create','fuel.drivers.edit','fuel.drivers.delete','fuel.drivers.recover']::text[]
 WHEN 'tolls.fleet.write' THEN ARRAY['tolls.fleet.create','tolls.fleet.edit','tolls.fleet.delete','tolls.fleet.recover']::text[]
 WHEN 'weighmytruck.write' THEN ARRAY['weighmytruck.create','weighmytruck.delete','weighmytruck.sync','weighmytruck.link','weighmytruck.recover']::text[]
 ELSE ARRAY[old] END) expanded WHERE expanded=ANY(ARRAY['fuel.drivers.sync','fuel.drivers.recover','tolls.fleet.sync','drivers.read','drivers.create','drivers.edit','drivers.delete','trucks.read','trucks.create','trucks.edit','trucks.delete','dispatchers.read','dispatchers.create','dispatchers.edit','dispatchers.delete','updaters.read','updaters.create','updaters.edit','updaters.delete','investors.read','investors.create','investors.edit','drivers.private.read','drivers.private.edit','drivers.documents.read','drivers.documents.edit','trucks.documents.read','trucks.documents.edit','driver_compensation.read','driver_compensation.edit','dispatcher_compensation.read','dispatcher_compensation.edit','investor_terms.read','investor_terms.edit','driver_pay.read','driver_pay.edit','driver_pay.finalize','driver_pay.reopen','investor_pay.read','investor_pay.edit','investor_pay.finalize','investor_pay.reopen','dispatcher_pay.read','driver_board.read','driver_board.write','board.read','board.write','loads.read','loads.sync','fuel.read','fuel.sync','fuel.drivers.create','fuel.drivers.edit','fuel.drivers.delete','tolls.read','tolls.sync','tolls.link','tolls.fleet.create','tolls.fleet.edit','tolls.fleet.delete','tolls.fleet.recover','weighmytruck.read','weighmytruck.create','weighmytruck.delete','weighmytruck.sync','weighmytruck.link','weighmytruck.recover','escrow.read','escrow.create','escrow.edit','escrow.delete','charges.read','charges.create','charges.edit','charges.delete','tasks.read','tasks.create','tasks.edit','tasks.delete','tasks.review','fleetscope.read','fleetscope.manage','fleetscope.reconcile','reports.read','expense_settings.manage','expenses.accounting.verify','access.manage']::text[]) ORDER BY expanded),version=version+1 WHERE NOT system_role;
UPDATE app_roles SET permissions=array_append(permissions,'expenses.accounting.verify') WHERE NOT system_role AND lower(trim(name))='accountant' AND NOT ('expenses.accounting.verify'=ANY(permissions));
-- Remove actions whose prerequisites were not granted; never add read access to repair old writes.
UPDATE app_roles SET permissions=array_remove(permissions,'drivers.create') WHERE NOT system_role AND NOT ARRAY['drivers.read']::text[] <@ permissions;
UPDATE app_roles SET permissions=array_remove(permissions,'drivers.edit') WHERE NOT system_role AND NOT ARRAY['drivers.read']::text[] <@ permissions;
UPDATE app_roles SET permissions=array_remove(permissions,'drivers.delete') WHERE NOT system_role AND NOT ARRAY['drivers.read']::text[] <@ permissions;
UPDATE app_roles SET permissions=array_remove(permissions,'trucks.create') WHERE NOT system_role AND NOT ARRAY['trucks.read']::text[] <@ permissions;
UPDATE app_roles SET permissions=array_remove(permissions,'trucks.edit') WHERE NOT system_role AND NOT ARRAY['trucks.read']::text[] <@ permissions;
UPDATE app_roles SET permissions=array_remove(permissions,'trucks.delete') WHERE NOT system_role AND NOT ARRAY['trucks.read']::text[] <@ permissions;
UPDATE app_roles SET permissions=array_remove(permissions,'dispatchers.create') WHERE NOT system_role AND NOT ARRAY['dispatchers.read']::text[] <@ permissions;
UPDATE app_roles SET permissions=array_remove(permissions,'dispatchers.edit') WHERE NOT system_role AND NOT ARRAY['dispatchers.read']::text[] <@ permissions;
UPDATE app_roles SET permissions=array_remove(permissions,'dispatchers.delete') WHERE NOT system_role AND NOT ARRAY['dispatchers.read']::text[] <@ permissions;
UPDATE app_roles SET permissions=array_remove(permissions,'updaters.create') WHERE NOT system_role AND NOT ARRAY['updaters.read']::text[] <@ permissions;
UPDATE app_roles SET permissions=array_remove(permissions,'updaters.edit') WHERE NOT system_role AND NOT ARRAY['updaters.read']::text[] <@ permissions;
UPDATE app_roles SET permissions=array_remove(permissions,'updaters.delete') WHERE NOT system_role AND NOT ARRAY['updaters.read']::text[] <@ permissions;
UPDATE app_roles SET permissions=array_remove(permissions,'investors.create') WHERE NOT system_role AND NOT ARRAY['investors.read']::text[] <@ permissions;
UPDATE app_roles SET permissions=array_remove(permissions,'investors.edit') WHERE NOT system_role AND NOT ARRAY['investors.read']::text[] <@ permissions;
UPDATE app_roles SET permissions=array_remove(permissions,'drivers.private.read') WHERE NOT system_role AND NOT ARRAY['drivers.read']::text[] <@ permissions;
UPDATE app_roles SET permissions=array_remove(permissions,'drivers.private.edit') WHERE NOT system_role AND NOT ARRAY['drivers.read','drivers.private.read']::text[] <@ permissions;
UPDATE app_roles SET permissions=array_remove(permissions,'drivers.documents.read') WHERE NOT system_role AND NOT ARRAY['drivers.read']::text[] <@ permissions;
UPDATE app_roles SET permissions=array_remove(permissions,'drivers.documents.edit') WHERE NOT system_role AND NOT ARRAY['drivers.read','drivers.documents.read']::text[] <@ permissions;
UPDATE app_roles SET permissions=array_remove(permissions,'trucks.documents.read') WHERE NOT system_role AND NOT ARRAY['trucks.read']::text[] <@ permissions;
UPDATE app_roles SET permissions=array_remove(permissions,'trucks.documents.edit') WHERE NOT system_role AND NOT ARRAY['trucks.read','trucks.documents.read']::text[] <@ permissions;
UPDATE app_roles SET permissions=array_remove(permissions,'driver_compensation.edit') WHERE NOT system_role AND NOT ARRAY['driver_compensation.read']::text[] <@ permissions;
UPDATE app_roles SET permissions=array_remove(permissions,'dispatcher_compensation.edit') WHERE NOT system_role AND NOT ARRAY['dispatcher_compensation.read']::text[] <@ permissions;
UPDATE app_roles SET permissions=array_remove(permissions,'investor_terms.edit') WHERE NOT system_role AND NOT ARRAY['investor_terms.read']::text[] <@ permissions;
UPDATE app_roles SET permissions=array_remove(permissions,'driver_pay.edit') WHERE NOT system_role AND NOT ARRAY['driver_pay.read']::text[] <@ permissions;
UPDATE app_roles SET permissions=array_remove(permissions,'driver_pay.finalize') WHERE NOT system_role AND NOT ARRAY['driver_pay.read']::text[] <@ permissions;
UPDATE app_roles SET permissions=array_remove(permissions,'driver_pay.reopen') WHERE NOT system_role AND NOT ARRAY['driver_pay.read']::text[] <@ permissions;
UPDATE app_roles SET permissions=array_remove(permissions,'investor_pay.edit') WHERE NOT system_role AND NOT ARRAY['investor_pay.read']::text[] <@ permissions;
UPDATE app_roles SET permissions=array_remove(permissions,'investor_pay.finalize') WHERE NOT system_role AND NOT ARRAY['investor_pay.read']::text[] <@ permissions;
UPDATE app_roles SET permissions=array_remove(permissions,'investor_pay.reopen') WHERE NOT system_role AND NOT ARRAY['investor_pay.read']::text[] <@ permissions;
UPDATE app_roles SET permissions=array_remove(permissions,'driver_board.write') WHERE NOT system_role AND NOT ARRAY['driver_board.read']::text[] <@ permissions;
UPDATE app_roles SET permissions=array_remove(permissions,'board.write') WHERE NOT system_role AND NOT ARRAY['board.read']::text[] <@ permissions;
UPDATE app_roles SET permissions=array_remove(permissions,'loads.sync') WHERE NOT system_role AND NOT ARRAY['loads.read']::text[] <@ permissions;
UPDATE app_roles SET permissions=array_remove(permissions,'fuel.sync') WHERE NOT system_role AND NOT ARRAY['fuel.read']::text[] <@ permissions;
UPDATE app_roles SET permissions=array_remove(permissions,'fuel.drivers.create') WHERE NOT system_role AND NOT ARRAY['fuel.read']::text[] <@ permissions;
UPDATE app_roles SET permissions=array_remove(permissions,'fuel.drivers.edit') WHERE NOT system_role AND NOT ARRAY['fuel.read']::text[] <@ permissions;
UPDATE app_roles SET permissions=array_remove(permissions,'fuel.drivers.delete') WHERE NOT system_role AND NOT ARRAY['fuel.read']::text[] <@ permissions;
UPDATE app_roles SET permissions=array_remove(permissions,'tolls.sync') WHERE NOT system_role AND NOT ARRAY['tolls.read']::text[] <@ permissions;
UPDATE app_roles SET permissions=array_remove(permissions,'tolls.link') WHERE NOT system_role AND NOT ARRAY['tolls.read']::text[] <@ permissions;
UPDATE app_roles SET permissions=array_remove(permissions,'tolls.fleet.create') WHERE NOT system_role AND NOT ARRAY['tolls.read']::text[] <@ permissions;
UPDATE app_roles SET permissions=array_remove(permissions,'tolls.fleet.edit') WHERE NOT system_role AND NOT ARRAY['tolls.read']::text[] <@ permissions;
UPDATE app_roles SET permissions=array_remove(permissions,'tolls.fleet.delete') WHERE NOT system_role AND NOT ARRAY['tolls.read']::text[] <@ permissions;
UPDATE app_roles SET permissions=array_remove(permissions,'tolls.fleet.recover') WHERE NOT system_role AND NOT ARRAY['tolls.read']::text[] <@ permissions;
UPDATE app_roles SET permissions=array_remove(permissions,'weighmytruck.create') WHERE NOT system_role AND NOT ARRAY['weighmytruck.read']::text[] <@ permissions;
UPDATE app_roles SET permissions=array_remove(permissions,'weighmytruck.delete') WHERE NOT system_role AND NOT ARRAY['weighmytruck.read']::text[] <@ permissions;
UPDATE app_roles SET permissions=array_remove(permissions,'weighmytruck.sync') WHERE NOT system_role AND NOT ARRAY['weighmytruck.read']::text[] <@ permissions;
UPDATE app_roles SET permissions=array_remove(permissions,'weighmytruck.link') WHERE NOT system_role AND NOT ARRAY['weighmytruck.read']::text[] <@ permissions;
UPDATE app_roles SET permissions=array_remove(permissions,'weighmytruck.recover') WHERE NOT system_role AND NOT ARRAY['weighmytruck.read']::text[] <@ permissions;
UPDATE app_roles SET permissions=array_remove(permissions,'escrow.create') WHERE NOT system_role AND NOT ARRAY['escrow.read']::text[] <@ permissions;
UPDATE app_roles SET permissions=array_remove(permissions,'escrow.edit') WHERE NOT system_role AND NOT ARRAY['escrow.read']::text[] <@ permissions;
UPDATE app_roles SET permissions=array_remove(permissions,'escrow.delete') WHERE NOT system_role AND NOT ARRAY['escrow.read']::text[] <@ permissions;
UPDATE app_roles SET permissions=array_remove(permissions,'charges.create') WHERE NOT system_role AND NOT ARRAY['charges.read']::text[] <@ permissions;
UPDATE app_roles SET permissions=array_remove(permissions,'charges.edit') WHERE NOT system_role AND NOT ARRAY['charges.read']::text[] <@ permissions;
UPDATE app_roles SET permissions=array_remove(permissions,'charges.delete') WHERE NOT system_role AND NOT ARRAY['charges.read']::text[] <@ permissions;
UPDATE app_roles SET permissions=array_remove(permissions,'tasks.create') WHERE NOT system_role AND NOT ARRAY['tasks.read']::text[] <@ permissions;
UPDATE app_roles SET permissions=array_remove(permissions,'tasks.edit') WHERE NOT system_role AND NOT ARRAY['tasks.read']::text[] <@ permissions;
UPDATE app_roles SET permissions=array_remove(permissions,'tasks.delete') WHERE NOT system_role AND NOT ARRAY['tasks.read']::text[] <@ permissions;
UPDATE app_roles SET permissions=array_remove(permissions,'tasks.review') WHERE NOT system_role AND NOT ARRAY['tasks.read']::text[] <@ permissions;
UPDATE app_roles SET permissions=array_remove(permissions,'fleetscope.manage') WHERE NOT system_role AND NOT ARRAY['fleetscope.read']::text[] <@ permissions;
UPDATE app_roles SET permissions=array_remove(permissions,'fleetscope.reconcile') WHERE NOT system_role AND NOT ARRAY['fleetscope.read']::text[] <@ permissions;
UPDATE app_roles SET permissions=array_remove(array_remove(permissions,'fuel.drivers.sync'),'fuel.drivers.recover') WHERE NOT system_role AND NOT ('fuel.read'=ANY(permissions));
UPDATE app_roles SET permissions=array_remove(permissions,'tolls.fleet.sync') WHERE NOT system_role AND NOT ('tolls.read'=ANY(permissions));
UPDATE role_expense_category_access SET can_create=false,can_edit=false,can_delete=false WHERE NOT can_view;
INSERT INTO access_audit(actor_id,action,target_id,details) SELECT NULL,'permissions_v2_migration',r.id,jsonb_build_object('before',b.permissions,'after',r.permissions,'categoriesBefore',b.categories,'categoriesAfter',coalesce((SELECT jsonb_agg(to_jsonb(a) ORDER BY a.category_id) FROM role_expense_category_access a WHERE a.role_id=r.id),'[]'::jsonb),'reviewRequired',true,'reason','Review private details, documents and combined setup prerequisites') FROM app_roles r JOIN permission_before b USING(id);
DELETE FROM auth_sessions;
ALTER TABLE files ADD COLUMN IF NOT EXISTS uploaded_by uuid REFERENCES app_users(id);
COMMIT;

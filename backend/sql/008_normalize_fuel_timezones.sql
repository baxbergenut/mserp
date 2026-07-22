BEGIN;

UPDATE fuel_transactions
SET timezone = CASE timezone
    WHEN 'US/Eastern' THEN 'America/New_York'
    WHEN 'US/Central' THEN 'America/Chicago'
    WHEN 'US/Mountain' THEN 'America/Denver'
    WHEN 'US/Pacific' THEN 'America/Los_Angeles'
    WHEN 'US/Arizona' THEN 'America/Phoenix'
    WHEN 'US/Alaska' THEN 'America/Anchorage'
    WHEN 'US/Aleutian' THEN 'America/Adak'
    WHEN 'US/Hawaii' THEN 'Pacific/Honolulu'
    WHEN 'US/East-Indiana' THEN 'America/Indiana/Indianapolis'
    WHEN 'US/Indiana-Starke' THEN 'America/Indiana/Knox'
    WHEN 'US/Michigan' THEN 'America/Detroit'
    WHEN 'US/Samoa' THEN 'Pacific/Pago_Pago'
    ELSE timezone
END
WHERE timezone IN (
    'US/Eastern', 'US/Central', 'US/Mountain', 'US/Pacific', 'US/Arizona',
    'US/Alaska', 'US/Aleutian', 'US/Hawaii', 'US/East-Indiana',
    'US/Indiana-Starke', 'US/Michigan', 'US/Samoa'
);

COMMIT;

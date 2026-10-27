package repository

// Match each relevant number once. Search all records for that number so an
// older duplicate remains ambiguous; never impose a history cutoff on identity.
const payrollEntriesCTE = `pay_entries AS MATERIALIZED (
 SELECT * FROM ` + grossBoardEntriesSQL + ` e WHERE NOT e.deleted
 AND e.service_date >= $1::date AND e.service_date < $1::date+7
 AND ($3::uuid[] IS NULL OR e.driver_id=ANY($3::uuid[]))
), pay_load_numbers AS MATERIALIZED (
 SELECT n.number,CASE WHEN count(*)=1 THEN min(l.id) END id FROM (
 SELECT DISTINCT lower(btrim(load_number)) number FROM pay_entries
 WHERE load_record_id IS NULL AND load_number<>'' AND day_status=''
 ) n JOIN loads l ON lower(btrim(l.load_id))=n.number
 GROUP BY n.number
)`

const payrollResolvedLoad = `
 LEFT JOIN pay_load_numbers matched ON matched.number=lower(btrim(e.load_number))
 LEFT JOIN loads l ON l.id=coalesce(e.load_record_id,matched.id) AND NOT e.deleted AND e.day_status='' ` + loadTripSourceSQL

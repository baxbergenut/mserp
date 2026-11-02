#!/usr/bin/env python3
"""Restore all business data and referenced projection inputs into a disposable DB.

The immutable source rows fill only the derived tables omitted from the verified
backup restore; canonical data always comes from the backup's consistent snapshot.
No service, credential, source table or production financial record is changed.
"""
import argparse
import json
import os
from pathlib import Path
import re
import subprocess
import tempfile
import uuid


def command(*args, **kwargs):
    return subprocess.run(['runuser', '-u', 'postgres', '--', *args], check=True, **kwargs)


def query(database, sql):
    return command('psql', '-X', '-qAt', '-v', 'ON_ERROR_STOP=1', '-d', database,
                   '-c', sql, capture_output=True, text=True).stdout.strip()


def copy_rows(database, table, select):
    source = subprocess.Popen(['runuser', '-u', 'postgres', '--', 'psql', '-X', '-q',
                               '-v', 'ON_ERROR_STOP=1', '-d', 'mserp', '-c',
                               f'COPY ({select}) TO STDOUT'], stdout=subprocess.PIPE)
    try:
        command('psql', '-X', '-q', '-v', 'ON_ERROR_STOP=1', '-d', database,
                '-c', f'COPY {table} FROM STDIN', stdin=source.stdout)
    finally:
        source.stdout.close()
    if source.wait() != 0:
        raise RuntimeError('immutable source copy failed')


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--backup', type=Path, required=True)
    parser.add_argument('--migration', type=Path, required=True)
    parser.add_argument('--database', required=True)
    args = parser.parse_args()
    if not re.fullmatch(r'mserp_payroll_storage_[a-z0-9_]+_test', args.database):
        raise ValueError('a uniquely named disposable payroll storage _test DB is required')
    if not args.backup.is_file() or not args.migration.is_file():
        raise ValueError('backup and migration must exist')
    command('createdb', args.database)
    # Kept on failure for diagnosis; never overwrite/drop an existing database.
    command('pg_restore', '--no-owner', '--no-acl', '--section=pre-data',
            '--exit-on-error', '--dbname', args.database, str(args.backup))
    toc = command('pg_restore', '--list', str(args.backup), capture_output=True, text=True).stdout
    excluded = ('payroll_statement_versions', 'payroll_projection_generations',
                'payroll_routing_facts', 'payroll_load_facts')
    filtered = '\n'.join(';' + line if any(f'TABLE DATA public {name} ' in line for name in excluded)
                         else line for line in toc.splitlines())
    with tempfile.NamedTemporaryFile(mode='w', prefix='payroll-restore-', suffix='.list') as listing:
        listing.write(filtered)
        listing.flush()
        os.chmod(listing.name, 0o644)  # Table-of-contents metadata only.
        command('pg_restore', '--no-owner', '--no-acl', '--section=data', '--exit-on-error',
                '--use-list', listing.name, '--dbname', args.database, str(args.backup))
    values = json.loads(query(args.database, """SELECT json_build_object(
      'current',(SELECT coalesce(json_agg(version_id::text),'[]') FROM payroll_statement_state),
      'outputs',(SELECT coalesce(json_agg(version_id::text),'[]') FROM payroll_command_versions),
      'receipts',(SELECT coalesce(json_agg(json_build_object('expected',expected_revision,'result',result)),'[]') FROM payroll_command_receipts))"""))
    ids = set()

    def collect(value):
        if isinstance(value, str):
            try:
                ids.add(str(uuid.UUID(value)))
            except ValueError:
                pass
        elif isinstance(value, list):
            for child in value:
                collect(child)
        elif isinstance(value, dict):
            for child in value.values():
                collect(child)
    collect(values)
    literals = ','.join("'" + value + "'::uuid" for value in sorted(ids)) or 'NULL::uuid'
    copy_rows(args.database, 'payroll_statement_versions',
              f'SELECT * FROM payroll_statement_versions WHERE id IN ({literals})')
    generations = json.loads(query(args.database, """SELECT coalesce(json_agg(id),'[]') FROM (
      SELECT generation_id::text id FROM payroll_statement_versions UNION SELECT generation_id::text FROM payroll_projection_weeks) g"""))
    literals = ','.join("'" + str(uuid.UUID(value)) + "'::uuid" for value in generations) or 'NULL::uuid'
    copy_rows(args.database, 'payroll_projection_generations',
              f'SELECT * FROM payroll_projection_generations WHERE id IN ({literals})')
    command('pg_restore', '--no-owner', '--no-acl', '--section=post-data',
            '--exit-on-error', '--dbname', args.database, str(args.backup))
    # Hash all canonical financial/display rows and exact receipts, never print data.
    proof = """CREATE TEMP TABLE storage_business_proof(name text PRIMARY KEY,digest text);
    DO $$ DECLARE t text; digest text; BEGIN
      FOR t IN SELECT table_name FROM payroll_projection_sources WHERE classification IN ('financial','display')
        UNION SELECT unnest(ARRAY['payroll_command_receipts','payroll_command_versions','payroll_retirement_receipts']) LOOP
        EXECUTE format('SELECT md5(coalesce(string_agg(md5(to_jsonb(r)::text),'''' ORDER BY md5(to_jsonb(r)::text)),'''')) FROM %I r',t) INTO digest;
        INSERT INTO storage_business_proof VALUES(t,digest);
      END LOOP;
    END $$;
    """
    verification = """
    DO $$ DECLARE p record; actual text; BEGIN
      FOR p IN SELECT * FROM storage_business_proof LOOP
        EXECUTE format('SELECT md5(coalesce(string_agg(md5(to_jsonb(r)::text),'''' ORDER BY md5(to_jsonb(r)::text)),'''')) FROM %I r',p.name) INTO actual;
        IF actual IS DISTINCT FROM p.digest THEN RAISE EXCEPTION 'Business data changed: %',p.name; END IF;
      END LOOP;
    END $$;
    SELECT json_build_object('canonical_tables_unchanged',(SELECT count(*) FROM storage_business_proof),
      'current_statements',(SELECT count(*) FROM payroll_statement_current),
      'compact_audit_references',(SELECT count(*) FROM payroll_statement_references),
      'duplicate_statement_identities',(SELECT count(*) FROM (SELECT kind,statement_id,week_start FROM payroll_statement_current GROUP BY 1,2,3 HAVING count(*)>1) d),
      'database_bytes',pg_database_size(current_database()));
    """
    output = query(args.database, proof + args.migration.read_text() + verification)
    print(output)
    # Decode all compressed backup data to validate the stream without allocating
    # another 16 GB of redundant derived tables on this host.
    command('pg_restore', '--data-only', '--file=/dev/null', str(args.backup))
    print('Complete backup data stream decoded; business-data restore and cutover parity passed.')


if __name__ == '__main__':
    main()

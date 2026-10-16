#!/usr/bin/env python3
"""Operator-only bootstrap. Secret JSON arrives on stdin over verified SSH.

Installs the tested additive ownership migration and private receipt storage;
never initializes integration authority or applies a reconciliation batch.
"""
import base64
import datetime
import hashlib
import json
import os
import pathlib
import pwd
import secrets
import subprocess
import sys
import urllib.parse

ROOT = pathlib.Path('/opt/fleet-review')
CONFIG = pathlib.Path('/etc/fleet-review')
STATE = pathlib.Path('/var/lib/fleet-review')
BACKUP = pathlib.Path('/var/backups/fleet-review')
ROLE = 'fleet_review_portal'

def quote(value):
    return "'" + str(value).replace("'", "''") + "'"

def run(args, *, data=None, env=None):
    result = subprocess.run(args, input=data, capture_output=True, env=env)
    if result.returncode:
        # Never echo secret-bearing SQL, connection strings or stderr.
        raise RuntimeError('operator command failed: ' + args[0])
    return result.stdout

def main():
    payload = json.load(sys.stdin)
    company = payload['companyId']
    if payload['hostname'] != 'conflicts.msexpressinc.net' or company != '93ecdba8-e92f-4e2a-b134-8edf8d422170':
        raise RuntimeError('unexpected deployment scope')
    pairs = payload['policy'].get('rejectedPairs', [])
    if not isinstance(pairs,list) or any(not isinstance(pair,list) or len(pair)!=2 or any(not isinstance(value,str) for value in pair) for pair in pairs):
        raise RuntimeError('rejected identity pairs must be a nested list of two strings')
    if (CONFIG / 'config.json').exists():
        raise RuntimeError('portal already provisioned; preserve its credentials and state')
    for directory in [ROOT, CONFIG, STATE, BACKUP]:
        directory.mkdir(parents=True, exist_ok=True)
        os.chmod(directory, 0o700)
    ca = base64.b64decode(payload['ca'])
    if not ca.startswith(b'-----BEGIN CERTIFICATE-----'):
        raise RuntimeError('invalid database CA')
    (CONFIG / 'receiver-ca.crt').write_bytes(ca)
    os.chmod(CONFIG / 'receiver-ca.crt', 0o644)
    c = payload['connection']
    remote_env = os.environ.copy()
    remote_env.update(PGHOST=c['host'], PGPORT=str(c['port']), PGUSER=c['user'], PGPASSWORD=c['password'], PGDATABASE='fleetscope', PGSSLMODE='verify-full', PGSSLROOTCERT=str(CONFIG / 'receiver-ca.crt'), PGCONNECT_TIMEOUT='10')
    remote_cmd = ['psql', '-X', '-q', '-A', '-t', '-w', '-v', 'ON_ERROR_STOP=1']
    source_cmd = ['runuser', '-u', 'postgres', '--', 'psql', '-X', '-q', '-A', '-t', '-v', 'ON_ERROR_STOP=1', '--dbname=mserp']
    def remote(sql): return run(remote_cmd, data=sql.encode(), env=remote_env).decode().strip()
    def source(sql): return run(source_cmd, data=sql.encode()).decode().strip()
    identity = json.loads(remote("SELECT json_build_object('name',name,'dot',dot_number) FROM companies WHERE id=" + quote(company)))
    if identity != {'name': 'MS Express Inc', 'dot': '2812942'}:
        raise RuntimeError('receiver company verification failed')
    if source("SELECT full_name FROM investors WHERE is_company") != 'MS Express Inc.':
        raise RuntimeError('source company verification failed')
    stamp = datetime.datetime.now(datetime.timezone.utc).strftime('%Y%m%dT%H%M%SZ')
    for name,args,env in [('mserp-schema',['runuser','-u','postgres','--','pg_dump','--schema-only','--dbname=mserp'],None),('fleetscope-schema',['pg_dump','--schema-only','--no-owner','--no-privileges'],remote_env)]:
        backup=BACKUP / (stamp+'-'+name+'.sql')
        backup.write_bytes(run(args,env=env));os.chmod(backup,0o600)
    snapshot=remote("SELECT jsonb_build_object('trucks',(SELECT jsonb_agg(to_jsonb(v)) FROM vehicles v WHERE company_id="+quote(company)+"),'drivers',(SELECT jsonb_agg(jsonb_build_object('id',id,'name',first_name||' '||last_name,'status',status,'updatedAt',updated_at)) FROM drivers WHERE company_id="+quote(company)+"))")
    (BACKUP / (stamp+'-fleet-before.json')).write_text(snapshot)
    os.chmod(BACKUP / (stamp+'-fleet-before.json'),0o600)
    migration_name='20261015190000_mserp_fleet_sync.sql'
    migration=(ROOT / migration_name).read_bytes()
    checksum=hashlib.sha256(migration).hexdigest()
    if checksum!=payload['migrationSHA256']:
        raise RuntimeError('migration bytes differ from the tested candidate')
    if remote("SELECT to_regclass('public.mserp_sync_state') IS NOT NULL")=='f':
        remote("BEGIN; SET LOCAL lock_timeout='5s'; SET LOCAL statement_timeout='60s';\n"+migration.decode()+"\nINSERT INTO fleetscope_schema_migrations(name,checksum,baselined) VALUES("+quote(migration_name)+","+quote(checksum)+",false); COMMIT;")
    else:
        if remote("SELECT checksum FROM fleetscope_schema_migrations WHERE name="+quote(migration_name))!=checksum:
            raise RuntimeError('installed migration does not match the tested candidate')
    receipts=(ROOT/'portal-receipts.sql').read_text()
    source(receipts);remote(receipts)
    source_password=secrets.token_urlsafe(36);receiver_password=secrets.token_urlsafe(36)
    if source("SELECT count(*) FROM pg_roles WHERE rolname="+quote(ROLE))!='0' or remote("SELECT count(*) FROM pg_roles WHERE rolname="+quote(ROLE))!='0':
        raise RuntimeError('dedicated role already exists; do not rotate an unknown deployment')
    source("CREATE ROLE "+ROLE+" LOGIN PASSWORD "+quote(source_password)+" NOSUPERUSER NOCREATEDB NOCREATEROLE; GRANT mserp_app TO "+ROLE+"; GRANT USAGE ON SCHEMA fleet_review TO "+ROLE+"; GRANT SELECT,INSERT,UPDATE ON fleet_review.receipts TO "+ROLE+";")
    remote("CREATE ROLE "+ROLE+" LOGIN PASSWORD "+quote(receiver_password)+" NOSUPERUSER NOCREATEDB NOCREATEROLE; GRANT CONNECT ON DATABASE fleetscope TO "+ROLE+"; GRANT USAGE ON SCHEMA public,fleet_review TO "+ROLE+"; GRANT SELECT ON companies,drivers,vehicles,fleet_assignments,mserp_sync_state,mserp_sync_links,insurance_coverage_vehicles,insurance_policy_coverages,insurance_policies,files,trailers,cdls,medcards,mvrs,clearinghouse_queries,driver_application_documents,applicants,recruitment_processes,drug_test_orders,previous_employer_verifications,previous_employers,driver_consent_submissions,driver_compliance_stats TO "+ROLE+"; GRANT INSERT,UPDATE ON drivers,vehicles,fleet_assignments,driver_compliance_stats TO "+ROLE+"; GRANT UPDATE ON mserp_sync_state,mserp_sync_links TO "+ROLE+"; GRANT SELECT,INSERT,UPDATE ON fleet_review.receipts TO "+ROLE+"; GRANT INSERT ON entity_activities,mserp_sync_audit TO "+ROLE+"; GRANT USAGE,SELECT ON SEQUENCE mserp_sync_audit_id_seq TO "+ROLE+";")
    try: account=pwd.getpwnam('fleetreview')
    except KeyError:
        run(['useradd','--system','--home-dir',str(STATE),'--shell','/usr/sbin/nologin','fleetreview'])
        account=pwd.getpwnam('fleetreview')
    os.chown(STATE,account.pw_uid,account.pw_gid);os.chmod(STATE,0o700)
    os.chown(CONFIG,0,account.pw_gid);os.chmod(CONFIG,0o750)
    os.chmod(ROOT,0o755)
    receiver_url='postgres://'+ROLE+':'+urllib.parse.quote(receiver_password,safe='')+'@'+c['host']+':'+str(c['port'])+'/fleetscope?'+urllib.parse.urlencode({'sslmode':'verify-full','sslrootcert':str(CONFIG/'receiver-ca.crt'),'application_name':'fleet-review'})
    config={'sourceUrl':'postgres://'+ROLE+':'+urllib.parse.quote(source_password,safe='')+'@127.0.0.1:5432/mserp?sslmode=disable&application_name=fleet-review','receiverUrl':receiver_url,'companyId':company,'companyName':'MS Express Inc','sourceCompanyName':'MS Express Inc.','dot':'2812942','origin':'https://conflicts.msexpressinc.net','listen':'127.0.0.1:8096','directory':str(STATE),'username':payload['username'],'passwordHash':payload['passwordHash'],'allowApply':True,'policy':payload['policy']}
    path=CONFIG/'config.json';path.write_text(json.dumps(config,indent=2));os.chown(path,0,account.pw_gid);os.chmod(path,0o640)
    print(json.dumps({'configured':True,'schemaBackupDirectory':str(BACKUP),'integrationEnrolled':False,'reconciliationApplied':False}))

if __name__=='__main__':
    try: main()
    except Exception as error:
        print('Provisioning stopped: '+str(error),file=sys.stderr)
        sys.exit(1)

import importlib.machinery
import importlib.util
import os
from pathlib import Path
import tempfile
import unittest

loader = importlib.machinery.SourceFileLoader('routing', str(Path(__file__).with_name('mserp-db')))
spec = importlib.util.spec_from_loader(loader.name, loader)
routing = importlib.util.module_from_spec(spec)
loader.exec_module(routing)


class RoutingTests(unittest.TestCase):
    def setUp(self):
        directory = tempfile.TemporaryDirectory()
        self.addCleanup(directory.cleanup)
        self.root = Path(directory.name)
        self.env = self.root / 'app.env'
        self.admin = self.root / 'admin.conf'
        self.write(self.env, 'DATABASE_URL=postgres://app:secret@localhost:5432/mserp\n')

    def write(self, path, text):
        path.write_text(text)
        path.chmod(0o600)

    def destination(self):
        return routing.destination(self.env, self.admin, os.getuid())

    def remote(self):
        self.write(self.env, 'DATABASE_URL="postgres://app:secret@private.example:25060/mserp?sslmode=verify-full"\n')
        self.write(self.admin, '[mserp]\nhost=private.example\nport=25060\ndbname=mserp\nuser=doadmin\npassword=secret%123\nsslmode=verify-full\nsslrootcert=/etc/mserp/ca.crt\n')

    def test_local_does_not_use_staged_remote_credentials(self):
        self.write(self.admin, 'invalid remote configuration')
        self.assertEqual(self.destination(), ('local:mserp', None))
        args, environment = routing.command(['psql', '-X', '--dbname=mserp', '-c', 'SELECT 1'], None)
        self.assertEqual(args[:5], ['runuser', '-u', 'postgres', '--', 'psql'])
        self.assertFalse(any(k.startswith('PG') for k in environment))

    def test_managed_routing_keeps_credentials_out_of_arguments(self):
        self.remote()
        target, config = self.destination()
        self.assertTrue(target.startswith('remote:'))
        args, environment = routing.command(['pg_dump', '--no-owner', '--dbname=mserp'], config)
        self.assertEqual(args, ['pg_dump', '--no-owner'])
        self.assertEqual(environment['PGSERVICEFILE'], str(self.admin))
        self.assertNotIn('secret', str(args) + str(environment))

    def test_remote_missing_mismatched_or_unverified_admin_fails_closed(self):
        self.remote()
        original = self.admin.read_text()
        for old, new in [('private.example', 'other.example'), ('25060', '5432'), ('dbname=mserp', 'dbname=other'), ('verify-full', 'require')]:
            self.write(self.admin, original.replace(old, new))
            with self.assertRaises(ValueError): self.destination()
        self.admin.unlink()
        with self.assertRaises(FileNotFoundError): self.destination()

    def test_ambiguous_or_insecure_runtime_config_rejected(self):
        self.env.chmod(0o644)
        with self.assertRaises(ValueError): self.destination()
        self.write(self.env, 'DATABASE_URL=postgres://app@localhost/mserp\nDATABASE_URL=postgres://app@remote/mserp\n')
        with self.assertRaises(ValueError): self.destination()

    def test_target_identity_ignores_password_rotation(self):
        self.remote()
        target = self.destination()[0]
        self.write(self.admin, self.admin.read_text().replace('secret%123', 'new-password'))
        self.assertEqual(target, self.destination()[0])

    def test_libpq_incompatible_service_spacing_is_rejected_before_cutover(self):
        self.remote()
        self.write(self.admin, self.admin.read_text().replace('host=', 'host = '))
        with self.assertRaises(ValueError): self.destination()

    def test_connection_overrides_are_rejected(self):
        for option in ['--dbname=other', '--host=other', '-h', '-U', '--username=postgres']:
            with self.assertRaises(ValueError): routing.command(['psql', option], '/admin')


if __name__ == '__main__':
    unittest.main()

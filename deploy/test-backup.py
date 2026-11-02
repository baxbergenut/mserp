import contextlib
import datetime as dt
import fcntl
import importlib.machinery
import importlib.util
import io
import json
import os
from pathlib import Path
import signal
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch

loader = importlib.machinery.SourceFileLoader('backup', str(Path(__file__).with_name('mserp-backup')))
spec = importlib.util.spec_from_loader(loader.name, loader)
backup = importlib.util.module_from_spec(spec)
loader.exec_module(backup)


class BackupTests(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory()
        self.addCleanup(self.directory.cleanup)
        self.root = Path(self.directory.name)
        self.now = dt.datetime(2026, 11, 2, 12, tzinfo=dt.timezone.utc).timestamp()
        self.store = backup.Store(self.root, owner=os.getuid(), clock=lambda: self.now)
        self.catalog = patch.object(backup.subprocess, 'run')
        self.catalog_mock = self.catalog.start()
        self.addCleanup(self.catalog.stop)

    def file(self, stamp='20261102T110000Z', label='test'):
        path = self.root / f'mserp-{stamp}-{label}.dump'
        path.write_bytes(b'fixture-complete-dump')
        path.chmod(0o600)
        return path

    def seed(self):
        path = self.file()
        return path, self.store.verify(path)

    def test_freshness_uses_snapshot_start_not_recent_verification(self):
        path, record = self.seed()
        self.assertEqual(self.store.recent()['file'], path.name)
        self.now = record['snapshot_at'] + backup.DAY
        self.store.recent()
        self.now += 1
        with self.assertRaises(backup.BackupError):
            self.store.recent()

    def test_changed_missing_and_insecure_files_fail_closed(self):
        path, record = self.seed()
        path.write_bytes(b'changed-complete-dump')
        with self.assertRaises(backup.BackupError): self.store.recent()
        path.unlink()
        with self.assertRaises(backup.BackupError): self.store.recent()
        (self.root / 'latest-verified.json').unlink()
        path = self.file()
        self.store.verify(path)
        path.chmod(0o644)
        with self.assertRaises(backup.BackupError): self.store.recent()
        path.chmod(0o600)
        self.root.chmod(0o755)
        with self.assertRaises(backup.BackupError): self.store.recent()

    def test_receipt_paths_checksums_and_symlinks_rejected(self):
        path, record = self.seed()
        for change in [{'file': '../outside.dump'}, {'sha256': 'invalid'}, {'snapshot_at': self.now}]:
            invalid = dict(record, **change)
            with self.assertRaises(backup.BackupError): self.store.checked(invalid)
        path.unlink()
        target = self.root / 'target'
        target.write_bytes(b'fixture-complete-dump')
        path.symlink_to(target)
        with self.assertRaises(backup.BackupError): self.store.recent()

    def test_catalog_failure_or_changed_dump_does_not_replace_previous(self):
        path, previous = self.seed()
        new = self.file('20261102T120000Z')
        self.catalog_mock.side_effect = subprocess.CalledProcessError(1, 'pg_restore')
        with self.assertRaises(subprocess.CalledProcessError): self.store.verify(new)
        self.assertEqual(self.store.recent(), previous)
        self.catalog_mock.side_effect = lambda *a, **k: new.write_bytes(b'changed')
        with self.assertRaises(backup.BackupError): self.store.verify(new)
        self.assertEqual(self.store.recent(), previous)

    def test_older_or_future_backup_cannot_replace_latest(self):
        path, previous = self.seed()
        for stamp in ['20261101T120000Z', '20261103T120000Z']:
            with self.assertRaises(backup.BackupError): self.store.verify(self.file(stamp))
        self.assertEqual(self.store.recent(), previous)

    def test_retention_keeps_last_good_until_replacement_verified(self):
        old, previous = self.seed()
        unknown = self.file('20261030T120000Z', 'legacy')
        partial = self.root / 'unrelated.dump.partial'
        partial.write_text('pending')
        new = self.file('20261102T120000Z')
        record = self.store.verify(new)
        self.assertTrue(old.exists())
        self.store.prune(record)
        self.assertFalse(old.exists())
        self.assertTrue(new.exists())
        self.assertTrue(unknown.exists())
        self.assertTrue(partial.exists())
        self.assertEqual(self.store.recent(), record)

    def test_low_disk_preflight_does_not_start_dump_or_remove_backup(self):
        old, previous = self.seed()
        with patch.object(backup.shutil, 'disk_usage') as usage, patch.object(backup.subprocess, 'Popen') as spawn:
            usage.return_value.free = 0
            with self.assertRaises(backup.BackupError): self.store.create('test')
            spawn.assert_not_called()
        self.assertEqual(self.store.recent(), previous)

    def test_failed_dump_removes_partial_and_preserves_previous(self):
        old, previous = self.seed()
        class Failed:
            returncode = 1
            def poll(self): return 1
        with patch.object(backup.subprocess, 'Popen', return_value=Failed()):
            with self.assertRaises(backup.BackupError): self.store.create('new')
        self.assertEqual(self.store.recent(), previous)
        self.assertEqual(list(self.root.glob('*.partial')), [])

    def test_disk_reserve_stops_entire_process_group(self):
        old, previous = self.seed()
        class Running:
            pid = 98765
            returncode = None
            def poll(self): return self.returncode
            def wait(self, timeout=None): self.returncode = -15
        with patch.object(backup.subprocess, 'Popen', return_value=Running()), patch.object(backup.os, 'killpg') as kill, patch.object(backup.shutil, 'disk_usage') as usage:
            high = unittest.mock.Mock(free=10**12)
            low = unittest.mock.Mock(free=backup.RESERVE - 1)
            usage.side_effect = [high, low]
            with self.assertRaises(backup.BackupError): self.store.create('new')
            kill.assert_called_once_with(98765, signal.SIGTERM)
        self.assertEqual(self.store.recent(), previous)
        self.assertEqual(list(self.root.glob('*.partial')), [])

    def test_successful_creation_verifies_before_pruning(self):
        old, previous = self.seed()
        class Done:
            returncode = 0
            def poll(self): return 0
        def dump(*args, **kwargs):
            self.assertTrue(kwargs['start_new_session'])
            kwargs['stdout'].write(b'new-dump')
            return Done()
        with patch.object(backup.subprocess, 'Popen', side_effect=dump):
            record = self.store.create('new')
        self.assertFalse(old.exists())
        self.assertEqual(self.store.recent(), record)
        self.assertEqual((self.root / record['file']).read_bytes(), b'new-dump')

    def test_scheduled_run_reuses_recent_snapshot_and_serializes_writers(self):
        old, previous = self.seed()
        lock = self.root / 'backup.lock'
        with patch.object(backup, 'ROOT', self.root), patch.object(backup, 'LOCK', lock), patch.object(backup, 'Store', return_value=self.store), patch.object(backup.os, 'geteuid', return_value=0), patch.object(sys, 'argv', ['backup', '--scheduled']), patch.object(self.store, 'create') as create, contextlib.redirect_stdout(io.StringIO()):
            backup.main()
            create.assert_not_called()
            with lock.open('a') as held:
                fcntl.flock(held, fcntl.LOCK_EX | fcntl.LOCK_NB)
                with self.assertRaises(backup.BackupError): backup.main()
                create.assert_not_called()

    def test_daily_timer_does_not_skip_an_overnight_migration_backup(self):
        self.now = dt.datetime(2026, 11, 3, 8, 2, tzinfo=dt.timezone.utc).timestamp()
        record = self.store.verify(self.file('20261103T010000Z'))
        self.assertEqual(self.store.scheduled_reuse_age(), 0)
        lock = self.root / 'backup.lock'
        with patch.object(backup, 'ROOT', self.root), patch.object(backup, 'LOCK', lock), patch.object(backup, 'Store', return_value=self.store), patch.object(backup.os, 'geteuid', return_value=0), patch.object(sys, 'argv', ['backup', '--scheduled']), patch.object(self.store, 'create', return_value=record) as create, contextlib.redirect_stdout(io.StringIO()):
            backup.main()
            create.assert_called_once_with('scheduled')


if __name__ == '__main__':
    unittest.main()

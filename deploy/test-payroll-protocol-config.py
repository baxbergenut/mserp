#!/usr/bin/env python3
"""Exercise the exact embedded serializer using disposable configuration only."""
from pathlib import Path
import os
import stat
import subprocess
import sys
import tempfile


helper = Path(__file__).with_name("mserp-payroll-protocol").read_text(encoding="utf-8")
marker = "<<'PY'\n"
assert helper.count(marker) == 1, "expected one production configuration serializer"
script = helper.split(marker, 1)[1].split("\nPY\n", 1)[0]
preserved = [
    "# existing configuration remains unchanged",
    "ORIGINAL=preserved",
    "UNRELATED='value with spaces = and # characters'",
    "# PAYROLL_RESOURCE_LOCKS=comment",
    "DISPLAY_NAME=Clustie é",
    "",
]
old_flags = [
    "PAYROLL_RESOURCE_LOCKS=false",
    " export PAYROLL_RESOURCE_LOCKS = true",
    "PAYROLL_PREPARED_READS=false",
    "PAYROLL_PREPARED_COMMANDS=false",
    "PAYROLL_PREPARED_FINALIZATION=false",
    "PAYROLL_PROJECTION_MODE=off",
]
with tempfile.TemporaryDirectory(prefix="mserp-protocol-config-") as directory:
    config = Path(directory, "fixture.env")
    for target in ("resources", "dual", "global"):
        config.write_text("\n".join(preserved + old_flags) + "\n", encoding="utf-8")
        os.chmod(config, 0o640)
        before = config.stat()
        subprocess.run([sys.executable, "-", str(config), target], input=script,
                       text=True, check=True, encoding="utf-8")
        expected = preserved + [
            "PAYROLL_PROJECTION_MODE=shadow",
            "PAYROLL_PREPARED_READS=true",
            "PAYROLL_PREPARED_COMMANDS=true",
            "PAYROLL_PREPARED_FINALIZATION=true",
            "PAYROLL_RESOURCE_LOCKS=" + ("true" if target == "resources" else "false"),
        ]
        assert config.read_text(encoding="utf-8") == "\n".join(expected) + "\n", target
        after = config.stat()
        assert stat.S_IMODE(before.st_mode) == stat.S_IMODE(after.st_mode), "permissions changed"
        assert (before.st_uid, before.st_gid) == (after.st_uid, after.st_gid), "owner changed"
        print("PASS configuration", target)

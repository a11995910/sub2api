#!/usr/bin/env python3
"""使用临时 PostgreSQL 和 Redis 验证迁移 241 的真实回滚，不访问现有服务。"""

import hashlib
import importlib.machinery
import importlib.util
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import unittest

sys.dont_write_bytecode = True
REPO = Path(__file__).resolve().parents[2]
loader = importlib.machinery.SourceFileLoader("schema_compat", str(REPO / "deploy/release-schema-compat"))
spec = importlib.util.spec_from_loader(loader.name, loader)
helper = importlib.util.module_from_spec(spec)
loader.exec_module(helper)


class CommandStdinTest(unittest.TestCase):
    def test_commands_do_not_consume_remaining_script_input(self):
        # 外层进程模拟 ssh bash -s 的共享 stdin，内层命令模拟 docker exec -i。
        script = """
import importlib.machinery
import importlib.util
import sys
sys.dont_write_bytecode = True
loader = importlib.machinery.SourceFileLoader('compat', sys.argv[1])
spec = importlib.util.spec_from_loader(loader.name, loader)
module = importlib.util.module_from_spec(spec)
loader.exec_module(module)
reader = [sys.executable, '-c', 'import sys; print(sys.stdin.read())']
assert module.command(reader) == ''
assert module.command(reader, '显式 SQL 输入') == '显式 SQL 输入'
print(sys.stdin.read(), end='')
"""
        remaining = "echo 后续验证命令必须继续执行\n"
        result = subprocess.run([sys.executable, "-c", script, str(REPO / "deploy/release-schema-compat")],
                                input=remaining, text=True, capture_output=True, check=False)
        self.assertEqual(0, result.returncode, result.stderr)
        self.assertEqual(remaining, result.stdout)


class LocalRuntime:
    environment = "staging"

    def __init__(self, directory):
        self.directory = directory
        self.running = False

    def sql(self, sql):
        return helper.command(["psql", "-h", str(self.directory), "-p", "65432", "-d", "postgres",
                               "-X", "-qAt", "--set=ON_ERROR_STOP=1"], sql)

    def stopped_app_environment(self):
        helper.require(not self.running, "恢复前必须停止应用容器，防止并发写入")
        return {"REDIS_DB": "3"}

    def redis_command(self, args, data=None):
        return helper.command(["redis-cli", "-s", str(self.directory / "redis.sock")] + args, data)


class SchemaCompatTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        for binary in ("initdb", "pg_ctl", "psql", "redis-server", "redis-cli"):
            if not shutil.which(binary):
                raise unittest.SkipTest("隔离数据库验证缺少 " + binary)
        cls.temp = tempfile.TemporaryDirectory(prefix="s2-schema-", dir="/tmp")
        cls.directory = Path(cls.temp.name)
        cls.data = cls.directory / "data"
        cls.runtime = LocalRuntime(cls.directory)
        try:
            helper.command(["initdb", "-D", str(cls.data), "-A", "trust", "--no-locale", "-E", "UTF8"])
            helper.command(["pg_ctl", "-D", str(cls.data), "-l", str(cls.directory / "postgres.log"),
                            "-o", "-k " + str(cls.directory) + " -h '' -p 65432", "-w", "start"])
            helper.command(["redis-server", "--port", "0", "--unixsocket", str(cls.directory / "redis.sock"),
                            "--daemonize", "yes", "--pidfile", str(cls.directory / "redis.pid"),
                            "--logfile", str(cls.directory / "redis.log"), "--save", "", "--appendonly", "no"])
        except Exception:
            cls.tearDownClass()
            raise

    @classmethod
    def tearDownClass(cls):
        subprocess.run(["redis-cli", "-s", str(cls.directory / "redis.sock"), "shutdown", "nosave"],
                       capture_output=True)
        subprocess.run(["pg_ctl", "-D", str(cls.data), "-m", "immediate", "-w", "stop"], capture_output=True)
        cls.temp.cleanup()

    def setUp(self):
        self.path = self.directory / "snapshot.json"
        self.path.unlink(missing_ok=True)
        self.runtime.running = False
        self.runtime.sql("""
DROP SCHEMA public CASCADE; CREATE SCHEMA public;
CREATE TABLE groups(id bigint PRIMARY KEY, deleted_at timestamptz, unrelated_secret text);
CREATE TABLE api_keys(id bigint PRIMARY KEY, unrelated_secret text);
CREATE TABLE schema_migrations(filename text PRIMARY KEY, checksum text NOT NULL);
INSERT INTO groups(id,unrelated_secret) VALUES (1,'不应导出的业务凭据'),(2,'不应导出的业务凭据');
INSERT INTO api_keys(id,unrelated_secret) VALUES (1,'不应导出的业务凭据'),(2,'不应导出的业务凭据');
""")
        for filename in ("146_add_group_image_super_resolution.sql", "154_add_group_image_4k_enhancement.sql",
                         "155_add_group_image_2k_enhancement.sql", "156_add_group_image_4k_enhancement_model.sql",
                         "191_group_auto_fallback.sql"):
            self.runtime.sql((REPO / "backend/migrations" / filename).read_text())
        self.runtime.sql("""
UPDATE groups SET auto_fallback_group_id=2, image_super_resolution_enabled=true,
 image_2k_enhancement_enabled=true,image_2k_enhancement_group_id=2,
 image_4k_enhancement_enabled=true,image_4k_enhancement_group_id=2,
 image_4k_enhancement_model='模型''带引号' WHERE id=1;
UPDATE api_keys SET auto_group_fallback_enabled=false WHERE id=1;
""")

    def migrate(self, checksum=None):
        migration = (REPO / "backend/migrations" / helper.MIGRATION).read_bytes()
        checksum = checksum or hashlib.sha256(migration.decode().strip().encode()).hexdigest()
        self.runtime.sql(migration.decode() + "INSERT INTO schema_migrations VALUES (" +
                         helper.literal(helper.MIGRATION) + "," + helper.literal(checksum) + ");")

    def test_real_migration_restore_and_cache_isolation(self):
        before = helper.read_state(self.runtime)
        self.assertEqual(8, len(before["columns"]))
        self.assertEqual(1, len(before["constraints"]))
        self.assertEqual(1, len(before["indexes"]))
        self.assertTrue(helper.snapshot(self.runtime, self.path))
        self.assertEqual(0o600, self.path.stat().st_mode & 0o777)
        self.assertNotIn("不应导出的业务凭据", self.path.read_text())
        self.migrate()
        self.runtime.sql("INSERT INTO groups(id) VALUES(3); INSERT INTO api_keys(id) VALUES(3);")
        self.runtime.redis_command(["-n", "3", "SET", "apikey:auth:secret-key", "snapshot-v29"])
        self.runtime.redis_command(["-n", "3", "SET", "apikey:ratelimit:1", "12"])
        self.runtime.redis_command(["-n", "2", "SET", "apikey:auth:other-db", "keep"])
        self.assertTrue(helper.restore(self.runtime, self.path))
        restored = helper.read_state(self.runtime)
        for field in ("columns", "constraints", "indexes", "migration_checksum"):
            self.assertEqual(before[field], restored[field])
        for table in helper.COLUMNS:
            self.assertEqual(before["rows"][table], restored["rows"][table][:2])
        self.assertEqual("", self.runtime.redis_command(["-n", "3", "GET", "apikey:auth:secret-key"]))
        self.assertEqual("12", self.runtime.redis_command(["-n", "3", "GET", "apikey:ratelimit:1"]))
        self.assertEqual("keep", self.runtime.redis_command(["-n", "2", "GET", "apikey:auth:other-db"]))
        self.assertEqual("t", self.runtime.sql("SELECT auto_group_fallback_enabled FROM api_keys WHERE id=3"))
        # 重复恢复不覆盖恢复后发生的配置更新。
        self.runtime.sql("UPDATE api_keys SET auto_group_fallback_enabled=true WHERE id=1")
        self.assertTrue(helper.restore(self.runtime, self.path))
        self.assertEqual("t", self.runtime.sql("SELECT auto_group_fallback_enabled FROM api_keys WHERE id=1"))
        # 再次升级仍能应用原迁移，并安全生成无需恢复的快照。
        self.migrate()
        self.path.unlink()
        self.assertFalse(helper.snapshot(self.runtime, self.path))
        self.assertFalse(helper.restore(self.runtime, self.path))

    def test_deleted_target_group_preserves_foreign_key_semantics(self):
        helper.snapshot(self.runtime, self.path)
        self.migrate()
        self.runtime.sql("DELETE FROM groups WHERE id=2")
        helper.restore(self.runtime, self.path)
        self.assertEqual("t", self.runtime.sql("SELECT auto_fallback_group_id IS NULL FROM groups WHERE id=1"))

    def test_partial_and_changed_schema_are_rejected(self):
        self.runtime.sql("ALTER TABLE groups DROP COLUMN image_4k_enhancement_model")
        with self.assertRaisesRegex(RuntimeError, "部分存在"):
            helper.snapshot(self.runtime, self.path)
        self.assertFalse(self.path.exists())
        self.runtime.sql("ALTER TABLE groups ADD COLUMN image_4k_enhancement_model text")
        with self.assertRaisesRegex(RuntimeError, "定义"):
            helper.snapshot(self.runtime, self.path)

    def test_restore_rejects_active_app_bad_checksum_and_permissions(self):
        helper.snapshot(self.runtime, self.path)
        self.migrate("unknown")
        self.runtime.running = True
        with self.assertRaisesRegex(RuntimeError, "停止应用"):
            helper.restore(self.runtime, self.path)
        self.runtime.running = False
        with self.assertRaisesRegex(RuntimeError, "校验和"):
            helper.restore(self.runtime, self.path)
        self.path.chmod(0o644)
        with self.assertRaisesRegex(RuntimeError, "0600"):
            helper.restore(self.runtime, self.path)
        self.assertEqual([], helper.read_state(self.runtime)["columns"])

    def test_failed_ddl_rolls_back_all_restoration(self):
        helper.snapshot(self.runtime, self.path)
        self.migrate()
        self.runtime.sql("CREATE INDEX idx_groups_auto_fallback_group_id ON groups(id)")
        with self.assertRaisesRegex(RuntimeError, "运行时命令失败"):
            helper.restore(self.runtime, self.path)
        after = helper.read_state(self.runtime)
        self.assertEqual([], after["columns"])
        self.assertIsNotNone(after["migration_checksum"])

    def test_snapshot_refuses_overwrite(self):
        helper.snapshot(self.runtime, self.path)
        original = self.path.read_bytes()
        with self.assertRaises(FileExistsError):
            helper.snapshot(self.runtime, self.path)
        self.assertEqual(original, self.path.read_bytes())


if __name__ == "__main__":
    unittest.main(verbosity=2)

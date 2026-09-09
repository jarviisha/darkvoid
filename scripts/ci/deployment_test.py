#!/usr/bin/env python3
"""Behavioral Compose/release tests. Lifecycle commands use a recording fake."""
import fcntl
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import time
import unittest

REPO = Path(__file__).resolve().parents[2]
DOCKER = shutil.which("docker")
APP_DIGEST = "sha256:" + "a" * 64
BACKUP_DIGEST = "sha256:" + "b" * 64


class DeploymentTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.source_temp = tempfile.TemporaryDirectory()
        cls.source = Path(cls.source_temp.name)
        for name in ("scripts", "migrations"):
            shutil.copytree(REPO / name, cls.source / name)
        for name in REPO.glob("compose*.yml"):
            shutil.copy2(name, cls.source / name.name)
        for name in ("docker", "curl"):
            (cls.source / "scripts/ci/testdata" / name).chmod(0o755)
        cls.git("init", "-q")
        cls.git("add", ".")
        cls.git("-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "-qm", "fixture")
        cls.commit = cls.git("rev-parse", "HEAD").strip()

    @classmethod
    def tearDownClass(cls):
        cls.source_temp.cleanup()

    @classmethod
    def git(cls, *args):
        return subprocess.check_output(["git", *args], cwd=cls.source, text=True)

    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.root = Path(self.temp.name) / "darkvoid-dev"
        self.root.mkdir()
        self.env_file = self.root / ".env"
        self.env_file.write_text(
            "ENVIRONMENT=development\nDB_PASSWORD='test$p#word'\nJWT_SECRET=test-secret\n"
            "SERVER_PORT=9090\nSERVER_HOST=127.0.0.1\n"
            f"APP_DIGEST={APP_DIGEST}\nBACKUP_DIGEST={BACKUP_DIGEST}\n"
        )
        self.env = {"PATH": os.environ["PATH"], "DARKVOID_DIR": str(self.root),
                    "DARKVOID_RELEASE_DIR": str(REPO)}
        self.log = self.root / "calls.jsonl"
        self.counter = 0

    def tearDown(self):
        # Production bundles are read-only. Restore only our temporary fixtures.
        for path in self.root.rglob("*"):
            if not path.is_symlink():
                path.chmod(0o755 if path.is_dir() else 0o644)
        self.temp.cleanup()

    def run_command(self, args, *, env=None, cwd=None):
        return subprocess.run(args, env=env or self.env, cwd=cwd or REPO,
                              text=True, capture_output=True, timeout=30)

    def config(self, files=None, extra=None, profiles=()):
        env = self.env.copy()
        if files is not None:
            env["DARKVOID_COMPOSE"] = files
        env.update(extra or {})
        args = ["bash", str(REPO / "scripts/dv")]
        for profile in profiles:
            args += ["--profile", profile]
        result = self.run_command([*args, "config", "--format", "json"], env=env)
        self.assertEqual(result.returncode, 0, result.stderr)
        return json.loads(result.stdout)

    def test_listener_and_host_port_are_independent(self):
        for files in ("compose.yml", "compose.yml:compose.dev.yml", "compose.yml:compose.prod.yml"):
            with self.subTest(files=files):
                cfg = self.config(files)
                app = cfg["services"]["app"]
                self.assertEqual(app["environment"]["SERVER_HOST"], "0.0.0.0")
                self.assertEqual(app["environment"]["SERVER_PORT"], "8080")
                self.assertEqual(app["ports"][0]["target"], 8080)
                self.assertEqual(app["ports"][0]["published"], "9090")
                cfg = self.config(files, {"APP_HOST_PORT": "9091"})
                self.assertEqual(cfg["services"]["app"]["ports"][0]["published"], "9091")

    def test_app_only_and_legacy_external_profile(self):
        self.assertEqual(set(self.config("compose.yml")["services"]), {"app"})
        cfg = self.config(extra={"COMPOSE_PROFILES": "external"})
        self.assertEqual(set(cfg["services"]), {"app"})
        self.assertNotIn("depends_on", cfg["services"]["app"])

    def test_native_dotenv_selection_and_shell_override(self):
        with self.env_file.open("a") as stream:
            stream.write('# COMPOSE_FILE=docker-compose.prod.yml:docker-compose.codohue.yml\n')
        self.assertNotIn("codohue", self.config()["networks"])
        with self.env_file.open("a") as stream:
            stream.write('COMPOSE_FILE = "docker-compose.prod.yml:docker-compose.codohue.yml" # enabled\n')
        cfg = self.config(profiles=("tools",))
        self.assertTrue(cfg["networks"]["codohue"]["external"])
        for service in ("app", "ctl", "seed"):
            self.assertEqual(cfg["services"][service]["environment"]["DB_HOST"], "darkvoid-postgres")
            self.assertIn("codohue", cfg["services"][service]["networks"])
        cfg = self.config(extra={"COMPOSE_FILE": "compose.yml"})
        self.assertNotIn("codohue", cfg["networks"])
        self.assertEqual(set(cfg["services"]), {"app"})

    def test_allowlist_preserves_app_values_and_excludes_backup_secrets(self):
        (self.root / ".env.app").write_text("MAILER_FROM='Darkvoid <mail@example.invalid>'\n")
        (self.root / ".env.backup").write_text(
            "BACKUP_RESTIC_PASSWORD=backup-sentinel\nBACKUP_AWS_SECRET_ACCESS_KEY=backup-aws-sentinel\n"
        )
        cfg = self.config("compose.yml:compose.prod.yml", profiles=("tools",))
        for service in ("app", "ctl", "seed"):
            values = cfg["services"][service]["environment"]
            self.assertEqual(values["MAILER_FROM"], "Darkvoid <mail@example.invalid>")
            # Compose escapes dollars in canonical output so it can be re-read.
            self.assertEqual(values["DB_PASSWORD"].replace("$$", "$"), "test$p#word")
            self.assertFalse(any(key.startswith("BACKUP_") for key in values))
            self.assertNotIn("backup-sentinel", values.values())
            self.assertNotIn("backup-aws-sentinel", values.values())
            self.assertNotIn("env_file", cfg["services"][service])
        backup = cfg["services"]["pg-backup"]
        self.assertEqual(backup["environment"]["RESTIC_PASSWORD"], "backup-sentinel")
        self.assertGreater(int(backup["mem_limit"]), 0)
        self.assertGreater(float(backup["cpus"]), 0)

    def test_shared_image_and_single_safe_migration_job(self):
        for overlay in ("dev", "prod"):
            cfg = self.config(f"compose.yml:compose.{overlay}.yml", profiles=("tools",))
            services = cfg["services"]
            self.assertEqual(len({services[s]["image"] for s in ("app", "ctl", "seed")}), 1)
            self.assertEqual([s for s in services if s.startswith("migrate")], ["migrate"])
            self.assertEqual(services["migrate"]["restart"], "no")
            self.assertEqual(services["app"]["depends_on"]["migrate"]["condition"], "service_completed_successfully")
            self.assertNotIn("ports", services["postgres"])
            self.assertNotIn("ports", services["redis"])

    def test_make_seed_reset_preserves_resolved_counts(self):
        cases = (
            ("defaults", "", {}, [], (500, 40, 5)),
            ("dotenv", "SEED_POSTS=12\nSEED_LIKES_PER_POST=3\nSEED_COMMENTS_PER_POST=2\n",
             {}, [], (12, 3, 2)),
            ("quoted dotenv", 'SEED_POSTS = "12" # posts\nSEED_LIKES_PER_POST=3\nSEED_COMMENTS_PER_POST=2\n',
             {}, [], (12, 3, 2)),
            ("shell override", "SEED_POSTS=12\nSEED_LIKES_PER_POST=3\nSEED_COMMENTS_PER_POST=2\n",
             {"SEED_POSTS": "7"}, [], (7, 3, 2)),
            ("make override", "SEED_POSTS=12\nSEED_LIKES_PER_POST=3\nSEED_COMMENTS_PER_POST=2\n",
             {"SEED_POSTS": "7"}, ["SEED_POSTS=9"], (9, 3, 2)),
        )
        original = self.env_file.read_text()
        for name, dotenv, overrides, make_args, counts in cases:
            with self.subTest(name=name):
                self.env_file.write_text(original + dotenv)
                self.log.write_text("")
                env = self.deploy_env()
                env.update(DARKVOID_RELEASE_DIR=str(REPO),
                           DARKVOID_COMPOSE="compose.yml:compose.dev.yml", **overrides)
                result = self.run_command(
                    ["make", "--no-print-directory", "-f", str(REPO / "Makefile"),
                     f"DOCKER_COMPOSE=bash {REPO / 'scripts/dv'}",
                     "docker-seed-reset", *make_args], env=env, cwd=self.root)
                self.assertEqual(result.returncode, 0, result.stderr)
                runs = [call for call in self.calls() if "run" in call]
                self.assertEqual(len(runs), 1)
                args = runs[0][runs[0].index("seed") + 1:]
                posts, likes, comments = counts
                self.assertCountEqual(args, ["--reset", f"--posts={posts}",
                                            f"--likes-per-post={likes}",
                                            f"--comments-per-post={comments}"])

    def test_relative_server_override_and_ctl_use_same_selection(self):
        (self.root / "site.yml").write_text("services:\n  app:\n    environment:\n      LOG_LEVEL: debug\n")
        cfg = self.config("compose.yml:compose.prod.yml:site.yml")
        self.assertEqual(cfg["services"]["app"]["environment"]["LOG_LEVEL"], "debug")
        with self.env_file.open("a") as stream:
            stream.write("COMPOSE_FILE=compose.yml:compose.prod.yml:compose.codohue.yml\n")
        env = self.deploy_env()
        env["DARKVOID_RELEASE_DIR"] = str(REPO)
        result = self.run_command(["bash", str(REPO / "scripts/dvctl"), "user", "list"], env=env)
        self.assertEqual(result.returncode, 0, result.stderr)
        call = self.calls()[-1]
        self.assertIn(str(REPO / "compose.codohue.yml"), call)
        self.assertIn("--no-deps", call)
        self.assertEqual(call[-3:], ["ctl", "user", "list"])

    def candidate(self, sequence=1, commit=None):
        self.counter += 1
        commit = commit or self.commit
        incoming = self.root / "incoming" / f"{commit}-10-{self.counter}"
        incoming.parent.mkdir(exist_ok=True)
        result = self.run_command(
            ["bash", str(REPO / "scripts/create-release.sh"), str(incoming), commit,
             str(sequence), APP_DIGEST, BACKUP_DIGEST], cwd=self.source)
        self.assertEqual(result.returncode, 0, result.stderr)
        return incoming

    def deploy_env(self, fail=""):
        env = self.env.copy()
        env.pop("DARKVOID_RELEASE_DIR")
        env.update(PATH=f"{self.source}/scripts/ci/testdata:{env['PATH']}",
                   REAL_DOCKER=DOCKER, DEPLOY_TEST_LOG=str(self.log), DEPLOY_TEST_FAIL=fail)
        return env

    def deploy(self, candidate, fail=""):
        return self.run_command(
            ["bash", str(REPO / "scripts/deploy-release.sh"), str(self.root), str(candidate)],
            env=self.deploy_env(fail))

    def calls(self):
        return [json.loads(line) for line in self.log.read_text().splitlines()] if self.log.exists() else []

    def test_success_promotes_complete_release_and_uses_configured_port(self):
        before = self.env_file.read_bytes()
        result = self.deploy(self.candidate())
        self.assertEqual(result.returncode, 0, result.stderr)
        current = (self.root / "current").resolve()
        self.assertTrue((current / "release.env").is_file())
        self.assertEqual(self.env_file.read_bytes(), before)
        calls = self.calls()
        self.assertTrue(any("http://127.0.0.1:9090/health" in call for call in calls))
        phases = [call[-1] for call in calls if "run" in call or "up" in call]
        self.assertEqual(phases, ["redis", "migrate", "app", "pg-backup"])
        cfg = self.config(extra={"DARKVOID_RELEASE_DIR": str(current), "APP_DIGEST": "stale"})
        self.assertTrue(cfg["services"]["app"]["image"].endswith(APP_DIGEST))
        self.assertEqual(cfg["volumes"]["postgres_data"]["name"], "darkvoid-dev_postgres_data")

    def test_failures_do_not_promote_or_modify_operator_env(self):
        self.assertEqual(self.deploy(self.candidate()).returncode, 0)
        old = (self.root / "current").resolve()
        with self.env_file.open("a") as stream:
            stream.write("ENVIRONMENT=production\nDEPLOY_REQUIRE_BACKUP=false\n")
        before = self.env_file.read_bytes()
        for phase in ("migrate", "app", "curl", "pg-backup"):
            with self.subTest(phase=phase):
                self.log.write_text("")
                result = self.deploy(self.candidate(2), phase)
                self.assertNotEqual(result.returncode, 0)
                self.assertEqual((self.root / "current").resolve(), old)
                self.assertEqual(self.env_file.read_bytes(), before)
                if phase == "migrate":
                    self.assertFalse(any("up" in call and call[-1] == "app" for call in self.calls()))
                if phase == "pg-backup":
                    self.assertIn("--wait", self.calls()[-1])

    def test_checksum_and_older_release_rejected_before_lifecycle_calls(self):
        self.assertEqual(self.deploy(self.candidate(2)).returncode, 0)
        self.log.write_text("")
        result = self.deploy(self.candidate(1))
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("older", result.stderr)
        self.assertEqual(self.calls(), [])
        incoming = self.candidate(3)
        (incoming / "compose.prod.yml").write_text("tampered\n")
        self.assertNotEqual(self.deploy(incoming).returncode, 0)
        self.assertEqual(self.calls(), [])

    def test_different_history_rejected_even_with_a_newer_sequence(self):
        self.assertEqual(self.deploy(self.candidate()).returncode, 0)
        self.log.write_text("")
        # Same source tree, unrelated root commit: a newer CI number is insufficient.
        tree = self.git("rev-parse", f"{self.commit}^{{tree}}").strip()
        other = subprocess.check_output(
            ["git", "-c", "user.name=Test", "-c", "user.email=test@example.invalid",
             "commit-tree", tree, "-m", "unrelated history"], cwd=self.source, text=True).strip()
        result = self.deploy(self.candidate(2, other))
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("not an ancestor", result.stderr)
        self.assertEqual(self.calls(), [])

    def test_successful_second_release_records_previous(self):
        self.assertEqual(self.deploy(self.candidate()).returncode, 0)
        old = (self.root / "current").resolve()
        self.assertEqual(self.deploy(self.candidate(2)).returncode, 0)
        self.assertEqual((self.root / "previous").resolve(), old)
        self.assertNotEqual((self.root / "current").resolve(), old)

    def test_host_lock_serializes_deployment(self):
        incoming = self.candidate()
        with (self.root / ".deploy.lock").open("w") as lock:
            fcntl.flock(lock, fcntl.LOCK_EX)
            process = subprocess.Popen(
                ["bash", str(REPO / "scripts/deploy-release.sh"), str(self.root), str(incoming)],
                env=self.deploy_env(), stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
            try:
                time.sleep(0.2)
                self.assertIsNone(process.poll())
                self.assertFalse((self.root / "current").exists())
                self.assertEqual(self.calls(), [])
            finally:
                fcntl.flock(lock, fcntl.LOCK_UN)
                stdout, stderr = process.communicate(timeout=30)
            self.assertEqual(process.returncode, 0, stdout + stderr)

    def test_image_override_and_dynamic_deploy_port_fail_before_lifecycle_calls(self):
        with self.env_file.open("a") as stream:
            stream.write("COMPOSE_FILE=compose.yml:compose.prod.yml:site.yml\n")
        (self.root / "site.yml").write_text("services:\n  app:\n    image: wrong-image:latest\n")
        result = self.deploy(self.candidate())
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("release manifest", result.stderr)
        self.assertFalse((self.root / "current").exists())
        self.assertEqual(self.calls(), [])
        (self.root / "site.yml").write_text("services: {}\n")
        with self.env_file.open("a") as stream:
            stream.write("APP_HOST_PORT=0\n")
        result = self.deploy(self.candidate(2))
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("fixed HTTP host port", result.stderr)
        self.assertEqual(self.calls(), [])


if __name__ == "__main__":
    unittest.main()

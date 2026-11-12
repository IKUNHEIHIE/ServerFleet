"""Real signature/TLS tests plus isolated service transaction tests; no root/VPS."""

import argparse
import base64
import contextlib
import importlib.util
import io
import json
import os
import pathlib
import pwd
import shutil
import ssl
import subprocess
import tarfile
import tempfile
import threading
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location("fleet_setup", pathlib.Path(__file__).with_name("setup.py"))
setup = importlib.util.module_from_spec(spec)
spec.loader.exec_module(setup)


class SetupTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="sf-setup-test-")
        self.addCleanup(self.temp.cleanup)
        self.root = pathlib.Path(self.temp.name)
        self.addCleanup(self.make_writable)

    def make_writable(self):
        for directory, children, files in os.walk(self.root):
            pathlib.Path(directory).chmod(0o700)

    def archive(self, entries):
        filename = self.root / "input.tar.gz"
        with tarfile.open(filename, "w:gz") as tar:
            for name, data, kind in entries:
                info = tarfile.TarInfo(name)
                info.type = kind
                info.size = len(data)
                if kind in (tarfile.SYMTYPE, tarfile.LNKTYPE):
                    info.linkname = "/etc/passwd"
                    info.size = 0
                    tar.addfile(info)
                else:
                    tar.addfile(info, io.BytesIO(data))
        return filename

    def test_archive_rejects_traversal_links_duplicates_and_special_files(self):
        for name, kind in [("../outside", tarfile.REGTYPE), ("/etc/file", tarfile.REGTYPE),
                           ("safe/link", tarfile.SYMTYPE), ("safe/hard", tarfile.LNKTYPE),
                           ("safe/pipe", tarfile.FIFOTYPE), ("safe//file", tarfile.REGTYPE)]:
            with self.subTest(name=name, kind=kind):
                target = self.root / "extract"
                with self.assertRaises(setup.SetupError):
                    setup.extract(self.archive([(name, b"bad", kind)]), target)
                shutil.rmtree(target)
        with self.assertRaises(setup.SetupError):
            setup.extract(self.archive([("file", b"a", tarfile.REGTYPE),
                                        ("file", b"b", tarfile.REGTYPE)]), self.root / "extract")
        self.assertFalse((self.root.parent / "outside").exists())

    def test_archive_private_modes_not_archive_modes(self):
        archive = self.archive([("version/controller", b"bin", tarfile.REGTYPE)])
        setup.extract(archive, self.root / "unpacked")
        self.assertEqual((self.root / "unpacked/version/controller").stat().st_mode & 0o777, 0o600)
        self.assertEqual((self.root / "unpacked/version").stat().st_mode & 0o777, 0o700)

    def test_duplicate_json_and_no_overwrite(self):
        file = self.root / "json"
        setup.write_new(file, '{"format":1,"format":2}')
        with self.assertRaises(setup.SetupError):
            setup.read_json(file)
        with self.assertRaises(FileExistsError):
            setup.write_new(file, "overwritten")
        self.assertIn('"format":2', file.read_text())

    def test_exact_modes_even_under_private_umask(self):
        previous = os.umask(0o077)
        try:
            setup.write_new(self.root / "unit", "unit", 0o644)
            setup.write_new(self.root / "catalog", "catalog")
        finally:
            os.umask(previous)
        self.assertEqual((self.root / "unit").stat().st_mode & 0o777, 0o644)
        self.assertEqual((self.root / "catalog").stat().st_mode & 0o777, 0o600)

    def test_flags_and_systemd_escaping(self):
        self.assertEqual(setup.flag_values(["/bin/controller", "-db", "/db", "--origin=https://fleet.test:8443",
                                          "-disable-cleanup-retries"]),
                         {"db": "/db", "origin": "https://fleet.test:8443", "disable-cleanup-retries": "true"})
        for argv in [["/bin/controller", "-db"], ["/bin/controller", "-db=a", "-db=b"], ["/bin/controller", "supervise"]]:
            with self.assertRaises(setup.SetupError):
                setup.flag_values(argv)
        self.assertEqual(setup.systemd_word('/private/a $x %n "b"'), '"/private/a $$x %%n \\"b\\""')
        with self.assertRaises(setup.SetupError):
            setup.systemd_word("line\nbreak")

    def test_strict_origin_and_fixed_ips(self):
        for value in ["http://fleet.test", "https://fleet.test/", "https://u:p@fleet.test", "https://fleet.test?q=1",
                      "https://127.0.0.1", "https://[::1]", "https://fleet.test/#x"]:
            with self.assertRaises(setup.SetupError):
                setup.origin(value)
        self.assertEqual(setup.origin("https://fleet.test:8443").port, 8443)
        self.assertEqual(setup.addresses("192.0.2.1, 2001:db8::1"), ["192.0.2.1", "2001:db8::1"])
        for value in ["0.0.0.0", "ff02::1", "192.0.2.1,192.0.2.1"]:
            with self.assertRaises(setup.SetupError):
                setup.addresses(value)

    def fixture(self):
        key = self.root / "test-only.key"
        setup.run(["openssl", "genpkey", "-algorithm", "ED25519", "-out", key])
        public = setup.run(["openssl", "pkey", "-in", key, "-pubout", "-outform", "DER"])[-32:]
        bundle = self.root / "bundle"
        publish = bundle / "publish"
        publish.mkdir(parents=True)
        for arch in setup.ARCHIVES:
            directory = publish / "0.9.1" / arch
            directory.mkdir(parents=True)
            entries = []
            for name in ("controller", "agent"):
                data = (arch + name).encode()
                setup.write_new(directory / name, data)
                entry = {"path": name, "bytes": len(data), "sha256": setup.digest(data), "executable": True}
                entries.append(entry)
                manifest = {"format": 1, "component": name, "version": "0.9.1", "sequence": 2,
                            "os": "linux", "architecture": arch, "bytes": len(data), "sha256": entry["sha256"]}
                setup.write_json(directory / (name + ".manifest.json"), manifest)
            (directory / "web").mkdir()
            setup.write_new(directory / "web/index.html", "<html>test</html>")
            for name in ("controller.manifest.json", "agent.manifest.json", "web/index.html"):
                data = (directory / name).read_bytes()
                entries.append({"path": name, "bytes": len(data), "sha256": setup.digest(data), "executable": False})
            manifest = {"format": 1, "version": "0.9.1", "sequence": 2, "os": "linux", "architecture": arch, "files": entries}
            setup.write_json(directory / "installation.manifest.json", manifest)
            message = self.root / (arch + ".message")
            setup.write_new(message, setup.DOMAIN + (directory / "installation.manifest.json").read_bytes())
            setup.run(["openssl", "pkeyutl", "-sign", "-inkey", key, "-rawin", "-in", message,
                       "-out", directory / "installation.signature"])
        setup.write_new(publish / "install.sh", "#!/bin/sh\nexit 0\n")
        files = {file.relative_to(publish).as_posix(): {"bytes": file.stat().st_size, "sha256": setup.file_hash(file)}
                 for file in publish.rglob("*") if file.is_file()}
        trust = {"format": 1, "version": "0.9.1", "minimumSequence": 2, "keyId": "test-only",
                 "publicKey": base64.b64encode(public).decode(), "files": files}
        setup.write_json(bundle / "trust.json", trust)
        return bundle, trust

    def test_real_ed25519_bundle_and_tampering(self):
        bundle, trust = self.fixture()
        fingerprint = setup.file_hash(bundle / "trust.json")
        self.assertEqual(setup.verify_bundle(bundle, fingerprint), trust)
        for target in ("install.sh", "0.9.1/amd64/controller", "0.9.1/arm64/installation.signature"):
            file = bundle / "publish" / target
            saved = file.read_bytes()
            file.write_bytes(b"tamper")
            with self.subTest(target=target), self.assertRaises(setup.SetupError):
                setup.verify_bundle(bundle, fingerprint)
            file.write_bytes(saved)
        with self.assertRaises(setup.SetupError):
            setup.verify_bundle(bundle, "f" * 64)

    def test_extra_secret_never_published(self):
        bundle, trust = self.fixture()
        setup.write_new(bundle / "publish/release-private.key", "must not publish")
        with self.assertRaises(setup.SetupError):
            setup.verify_bundle(bundle, setup.file_hash(bundle / "trust.json"))

    def test_replaced_key_even_with_updated_inventory_fails_signature(self):
        bundle, trust = self.fixture()
        trust["publicKey"] = base64.b64encode(b"x" * 32).decode()
        (bundle / "trust.json").write_text(json.dumps(trust))
        with self.assertRaises(setup.SetupError):
            setup.verify_bundle(bundle, setup.file_hash(bundle / "trust.json"))

    def tls(self):
        key, ca = self.root / "https.key", self.root / "https.pem"
        setup.run(["openssl", "req", "-x509", "-newkey", "rsa:2048", "-nodes", "-keyout", key,
                   "-out", ca, "-days", "1", "-subj", "/CN=fleet.test", "-addext", "subjectAltName=DNS:fleet.test",
                   "-addext", "basicConstraints=critical,CA:TRUE"])
        return key, ca

    def test_real_tls_pinning_allowlist_head_and_no_redirects(self):
        key, ca = self.tls()
        directory = self.root / "public"
        directory.mkdir()
        setup.write_new(directory / "install.sh", "public")
        setup.write_new(directory / "secret.key", "NEVER SERVE")
        handler = setup.public_handler(directory, {"install.sh": {"bytes": 6, "sha256": setup.digest(b"public")}})
        server = setup.PublicServer(("127.0.0.1", 0), handler)
        context = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
        context.minimum_version = ssl.TLSVersion.TLSv1_3
        context.load_cert_chain(ca, key)
        server.socket = context.wrap_socket(server.socket, server_side=True, do_handshake_on_connect=False)
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        self.addCleanup(server.server_close)
        self.addCleanup(server.shutdown)
        base = "https://fleet.test:" + str(server.server_port)
        with patch.dict(os.environ, {"HTTPS_PROXY": "http://127.0.0.1:1"}):
            self.assertEqual(setup.pinned_get(base + "/install.sh", ["127.0.0.1"], ca.read_text(), 100), b"public")
        client_context = ssl.create_default_context(cafile=str(ca))
        connection = setup.http.client.HTTPSConnection("fleet.test", server.server_port, context=client_context, timeout=5)
        connection._create_connection = lambda address, timeout, source_address=None: setup.socket.create_connection(("127.0.0.1", address[1]), timeout)
        connection.request("HEAD", "/install.sh")
        response = connection.getresponse()
        self.assertEqual((response.status, response.getheader("Content-Length"), response.read()), (200, "6", b""))
        connection.close()
        for path in ("/secret.key", "/../https.key", "/%2e%2e/https.key", "/install.sh?q=secret", "/"):
            with self.subTest(path=path), self.assertRaises(setup.SetupError):
                setup.pinned_get(base + path, ["127.0.0.1"], ca.read_text(), 100)
        with self.assertRaises(setup.SetupError):
            setup.pinned_get(base + "/install.sh", ["127.0.0.1"], ca.read_text(), 1)
        with self.assertRaises(setup.SetupError):
            setup.pinned_get(base.replace("fleet.test", "other.test") + "/install.sh", ["127.0.0.1"], ca.read_text(), 100)
        public = setup.run(["openssl", "x509", "-in", ca, "-pubkey", "-noout"])
        spki = setup.run(["openssl", "pkey", "-pubin", "-outform", "DER"], public)
        self.assertEqual(setup.root_certificate(ca, setup.digest(spki)), ca.read_text())
        with self.assertRaises(setup.SetupError):
            setup.root_certificate(ca, "0" * 64)

    def test_offline_ack_required_before_keys_or_output(self):
        with self.assertRaises(setup.SetupError):
            setup.prepare(argparse.Namespace(offline_ack=False))
        self.assertEqual(list(self.root.iterdir()), [])

    def configure_fixture(self):
        bundle, trust = self.fixture()
        archive = self.root / "public.tar.gz"
        with tarfile.open(archive, "w:gz") as tar:
            tar.add(bundle / "trust.json", arcname="trust.json")
            for file in (bundle / "publish").rglob("*"):
                if file.is_file():
                    tar.add(file, arcname=file.relative_to(bundle).as_posix(), recursive=False)
        key, ca = self.tls()
        pki = self.root / "pki"
        pki.mkdir()
        shutil.copyfile(ca, pki / "root.pem")
        public = setup.run(["openssl", "x509", "-in", ca, "-pubkey", "-noout"])
        spki = setup.run(["openssl", "pkey", "-pubin", "-outform", "DER"], public)
        db = self.root / "existing.db"
        db.write_bytes(b"EXISTING DATABASE DO NOT MODIFY")
        values = {"db": str(db), "origin": "https://fleet.test:8443", "tls-cert": str(ca), "tls-key": str(key),
                  "agent-pki-dir": str(pki), "agent-root-pin": setup.digest(spki),
                  "agent-authority": "fleet.test:8443", "agent-server-name": "fleet.test"}
        argv = ["/trusted/controller"] + [part for pair in values.items() for part in ("-" + pair[0], pair[1])]
        base = self.root / "installed"
        systemd = self.root / "systemd"
        systemd.mkdir()
        args = argparse.Namespace(service="serverfleet-controller.service", bundle=str(archive),
                                  trust_sha256=setup.file_hash(bundle / "trust.json"), addresses="127.0.0.1",
                                  https_root=str(ca), directory=str(base), port=19444, check=False)
        return args, (systemd, base, values, argv, trust)

    def isolated_configure(self, args, state, fail_restart=False):
        systemd, base, values, argv, trust = state
        calls = []
        original = subprocess.run

        def subprocess_call(command, **kwargs):
            if str(command[0]) == "systemctl":
                calls.append(command)
                failed = fail_restart and command[1:3] == ["restart", args.service]
                return subprocess.CompletedProcess(command, 1 if failed else 0, b"", b"")
            return original(command, **kwargs)

        def get(url, ips, ca, limit):
            if url.endswith("/readyz"):
                return b'{"status":"ready"}'
            return (self.root / "bundle/publish/install.sh").read_bytes()

        user = pwd.getpwuid(os.geteuid())
        with patch.object(setup, "SYSTEMD_DIR", systemd), patch.object(setup.os, "geteuid", return_value=0), \
                patch.object(setup, "owned_root_parent"), \
                patch.object(setup, "controller_state", return_value=({}, user, argv, values)), \
                patch.object(setup.subprocess, "run", side_effect=subprocess_call), \
                patch.object(setup, "pinned_get", side_effect=get), contextlib.redirect_stdout(io.StringIO()), \
                contextlib.redirect_stderr(io.StringIO()):
            if fail_restart:
                with self.assertRaises(setup.SetupError):
                    setup.configure(args)
            else:
                setup.configure(args)
        return calls

    def test_configure_transaction_writes_catalog_and_preserves_database(self):
        args, state = self.configure_fixture()
        calls = self.isolated_configure(args, state)
        systemd, base, values, argv, trust = state
        private = base / "controller-config"
        catalog_file = private / "installation-catalog.json"
        catalog = setup.read_json(catalog_file)
        self.assertEqual(catalog["source"]["publicKey"], trust["publicKey"])
        self.assertEqual(catalog["source"]["origin"], "https://fleet.test:19444")
        self.assertEqual(catalog_file.stat().st_mode & 0o777, 0o600)
        self.assertEqual(private.stat().st_mode & 0o777, 0o700)
        self.assertEqual(base.stat().st_mode & 0o777, 0o755)
        self.assertEqual((base / "publish/install.sh").stat().st_mode & 0o777, 0o444)
        self.assertEqual(setup.read_json(base / "receipt.json")["state"], "READY_PENDING_NODE_ACCEPTANCE")
        self.assertTrue((systemd / setup.SERVICE).exists())
        self.assertIn(["systemctl", "enable", setup.SERVICE], calls)
        self.assertEqual(pathlib.Path(values["db"]).read_bytes(), b"EXISTING DATABASE DO NOT MODIFY")
        if os.environ.get("SF_SETUP_TEST_CONTROLLER"):
            # Exercise the real production parser, including its private parent
            # check. Python-only schema assertions cannot prove Go acceptance.
            source = self.root / "catalog-check.go"
            source.write_text('package main\nimport ("os"; "serverfleet.local/serverfleet/apps/controller/server")\n'
                              'func main(){_,e:=server.LoadInstallationCatalog(os.Args[1],os.Args[2],os.Args[3]);'
                              'if e!=nil{panic(e)}}\n')
            setup.run([os.environ.get("SF_SETUP_GO", "go"), "run", source, catalog_file,
                       values["origin"], values["agent-root-pin"]])

    def test_configure_failure_removes_only_new_dropin_and_restores_old_service(self):
        args, state = self.configure_fixture()
        systemd, base, values, argv, trust = state
        original = systemd / "serverfleet-controller.service"
        original.write_text("EXISTING UNIT")
        calls = self.isolated_configure(args, state, fail_restart=True)
        self.assertFalse((systemd / setup.SERVICE).exists())
        self.assertFalse((systemd / (args.service + ".d") / "90-serverfleet-installation.conf").exists())
        self.assertEqual(original.read_text(), "EXISTING UNIT")
        self.assertTrue((base / "receipt.json").exists())
        self.assertGreaterEqual(calls.count(["systemctl", "restart", args.service]), 2)
        self.assertEqual(pathlib.Path(values["db"]).read_bytes(), b"EXISTING DATABASE DO NOT MODIFY")

    def test_configure_check_has_no_service_changes(self):
        args, state = self.configure_fixture()
        args.check = True
        calls = self.isolated_configure(args, state)
        self.assertEqual(calls, [])
        self.assertFalse(state[1].exists())

    @unittest.skipUnless(os.environ.get("SF_SETUP_TEST_CONTROLLER"), "set SF_SETUP_TEST_CONTROLLER to the built Controller")
    def test_offline_packaging_with_real_controller_and_public_only_export(self):
        binary = pathlib.Path(os.environ["SF_SETUP_TEST_CONTROLLER"]).resolve()
        self.assertTrue(binary.is_file())
        bundle, trust = self.fixture()
        archives, pinned = {}, {}
        for arch in setup.ARCHIVES:
            directory = bundle / "publish/0.9.1" / arch
            (directory / "controller").write_bytes(binary.read_bytes())
            release = setup.read_json(directory / "controller.manifest.json")
            release.update(bytes=binary.stat().st_size, sha256=setup.file_hash(binary))
            (directory / "controller.manifest.json").write_text(json.dumps(release))
            manifest = setup.read_json(directory / "installation.manifest.json")
            for entry in manifest["files"]:
                file = directory / entry["path"]
                entry.update(bytes=file.stat().st_size, sha256=setup.file_hash(file))
            (directory / "installation.manifest.json").write_text(json.dumps(manifest))
            archive = self.root / (arch + ".tar.gz")
            with tarfile.open(archive, "w:gz") as tar:
                tar.add(directory, arcname="serverfleet_0.9.1_linux_" + arch)
            archives[arch] = str(archive)
            pinned[arch] = setup.file_hash(archive)
        key_directory = self.root / "offline-key"
        output = self.root / "export"
        args = argparse.Namespace(offline_ack=True, **archives, output=str(output), key_directory=str(key_directory),
                                  key_id="test-only", installer=str(bundle / "publish/install.sh"))
        with patch.object(setup, "ARCHIVES", pinned), contextlib.redirect_stdout(io.StringIO()):
            setup.prepare(args)
        self.assertTrue((key_directory / "release-private.key").is_file())
        with tarfile.open(output / "installation-public.tar.gz") as tar:
            self.assertFalse(any("private" in name or "offline-key" in name for name in tar.getnames()))
        unpacked = self.root / "export-check"
        setup.extract(output / "installation-public.tar.gz", unpacked)
        setup.verify_bundle(unpacked, setup.file_hash(output / "trust.json"))


if __name__ == "__main__":
    unittest.main()

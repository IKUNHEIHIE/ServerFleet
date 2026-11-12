#!/usr/bin/env python3
"""Guided, fail-closed setup for an existing Server Fleet Controller.

Only Python's standard library and the system OpenSSL/systemd tools are used.
Signing is an explicit offline operation; configure never receives a private
release key. Run without arguments for the Chinese guided menu.
"""

import argparse
import base64
import contextlib
import fcntl
import hashlib
import http.client
import http.server
import ipaddress
import json
import os
import pathlib
import platform
import pwd
import re
import shutil
import socket
import ssl
import stat
import subprocess
import sys
import tarfile
import tempfile
import threading
import time
import urllib.parse


class SetupError(Exception):
    pass


ROOT = pathlib.Path(__file__).resolve().parent
ARCHIVES = {
    "amd64": "fb09ca2cc567f61d85958703440c0b56f1fbda9d5c618a6800111266c99d7264",
    "arm64": "15b244e4a051ee763792d1f7083e5ccff7cc6ea70265e95d4ae7b31b4c96616a",
}
DOMAIN = b"serverfleet.installation.v1\0"
MAX_TOTAL = 650 << 20
SERVICE = "serverfleet-distribution.service"
SCRIPT = "setup.py"
SYSTEMD_DIR = pathlib.Path("/etc/systemd/system")


def require(condition, message):
    if not condition:
        raise SetupError(message)


def run(args, data=None):
    result = subprocess.run([str(a) for a in args], input=data,
                            stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                            timeout=300, check=False)
    # Do not propagate arbitrary program output (it might contain credentials).
    require(result.returncode == 0, f"命令失败：{pathlib.Path(args[0]).name}；已有数据保留。")
    return result.stdout


def digest(data):
    return hashlib.sha256(data).hexdigest()


def file_hash(path):
    h = hashlib.sha256()
    with open(path, "rb") as stream:
        for block in iter(lambda: stream.read(1 << 20), b""):
            h.update(block)
    return h.hexdigest()


def unique_object(pairs):
    result = {}
    for key, value in pairs:
        require(key not in result, "JSON 中存在重复字段。")
        result[key] = value
    return result


def read_json(path, limit=256 << 10):
    with path.open("rb") as stream:
        raw = stream.read(limit + 1)
    require(len(raw) <= limit, "JSON 超出大小限制。")
    return json.loads(raw, object_pairs_hook=unique_object)


def write_new(path, data, mode=0o600):
    flags = os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW
    fd = os.open(path, flags, mode)
    created = os.fstat(fd)
    try:
        with os.fdopen(fd, "wb") as stream:
            os.fchmod(stream.fileno(), mode)
            stream.write(data if isinstance(data, bytes) else data.encode())
            stream.flush()
            os.fsync(stream.fileno())
    except BaseException:
        with contextlib.suppress(OSError):
            if os.path.samestat(created, os.lstat(path)):
                os.unlink(path)
        raise


def write_json(path, value):
    write_new(path, json.dumps(value, ensure_ascii=False, sort_keys=True,
                              separators=(",", ":")) + "\n")


def relative_name(name):
    return (isinstance(name, str) and len(name) <= 240
            and re.fullmatch(r"[A-Za-z0-9_.\-/]+", name) is not None
            and all(part not in ("", ".", "..") and not part.startswith(".")
                    for part in name.split("/")))


def extract(archive, destination):
    """Never use tar.extractall: links, devices and traversal are rejected."""
    destination.mkdir(mode=0o700)
    with tarfile.open(archive, "r:gz") as tar:
        members = []
        seen, total = set(), 0
        for member in tar:
            require(len(members) < 4096, "压缩包文件过多。")
            members.append(member)
            name = member.name.rstrip("/") if member.isdir() else member.name
            require(relative_name(name) and name not in seen, "压缩包含重复或不安全路径。")
            require(member.isdir() or member.isreg(), "压缩包不得包含链接或特殊文件。")
            seen.add(name)
            require(0 <= member.size <= 256 << 20, "压缩包成员过大。")
            total += member.size
            require(total <= MAX_TOTAL, "压缩包解压大小超限。")
        for member in members:
            target = destination / member.name.rstrip("/")
            if member.isdir():
                target.mkdir(mode=0o700, parents=True, exist_ok=True)
            else:
                target.parent.mkdir(mode=0o700, parents=True, exist_ok=True)
                with tar.extractfile(member) as source:
                    fd = os.open(target, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
                    with os.fdopen(fd, "wb") as output:
                        shutil.copyfileobj(source, output, 1 << 20)


def ask(value, label, default=None):
    if value is not None:
        return value
    require(sys.stdin.isatty(), f"非交互模式缺少参数：{label}")
    answer = input(label + (f" [{default}]" if default else "") + ": ").strip()
    answer = answer or default
    require(bool(answer), f"必须填写：{label}")
    return answer


def absolute(value):
    path = pathlib.Path(value).expanduser().absolute()
    require(path == path.resolve(), "路径不能包含符号链接或 ..。")
    return path


def private_tree(path):
    require(path.is_dir() and not path.is_symlink(), "需要普通私有目录。")
    for item in [path, *path.rglob("*")]:
        info = item.lstat()
        require(info.st_uid == os.geteuid() and not stat.S_ISLNK(info.st_mode), "私有输入的所有者不匹配或含链接。")
        require(stat.S_ISDIR(info.st_mode) or stat.S_ISREG(info.st_mode) and info.st_nlink == 1,
                "私有输入包含特殊文件或硬链接。")
        item.chmod(0o700 if item.is_dir() or item.name in ("controller", "agent") else 0o600)


def manifest_files(directory, architecture):
    manifest = read_json(directory / "installation.manifest.json")
    require(set(manifest) == {"format", "version", "sequence", "os", "architecture", "files"}, "安装清单字段不完整。")
    require(manifest["format"] == 1 and manifest["os"] == "linux"
            and manifest["architecture"] == architecture
            and re.fullmatch(r"[0-9]+\.[0-9]+\.[0-9]+", manifest["version"])
            and type(manifest["sequence"]) is int and 0 < manifest["sequence"] <= 2**53 - 1,
            "安装版本、平台或序列无效。")
    files, total = {}, 0
    require(isinstance(manifest["files"], list) and 5 <= len(manifest["files"]) <= 1024, "安装清单文件数量无效。")
    for entry in manifest["files"]:
        require(set(entry) == {"path", "sha256", "bytes", "executable"}, "安装文件字段无效。")
        name = entry["path"]
        require(relative_name(name) and name not in files, "安装清单路径无效。")
        executable = name in ("controller", "agent")
        require(entry["executable"] is executable, "安装文件执行标记无效。")
        require(executable or name in ("controller.manifest.json", "agent.manifest.json", "web/index.html", "web/favicon.ico")
                or name.startswith("web/assets/") and name.count("/") == 2, "安装清单含非产品文件。")
        require(type(entry["bytes"]) is int and 0 < entry["bytes"] <= (256 << 20 if executable else 8 << 20), "安装文件大小无效。")
        path = directory / name
        require(path.is_file() and not path.is_symlink() and path.stat().st_size == entry["bytes"]
                and file_hash(path) == entry["sha256"], "安装文件摘要不匹配。")
        files[name] = entry
        total += entry["bytes"]
    require(total <= 600 << 20 and {"controller", "agent", "controller.manifest.json", "agent.manifest.json", "web/index.html"} <= files.keys(),
            "安装清单不完整或过大。")
    for component in ("controller", "agent"):
        release = read_json(directory / (component + ".manifest.json"), 16384)
        expected = {"format": 1, "component": component, "version": manifest["version"],
                    "sequence": manifest["sequence"], "os": "linux", "architecture": architecture,
                    "sha256": files[component]["sha256"], "bytes": files[component]["bytes"]}
        require(release == expected, "组件与整体安装清单不一致。")
    return manifest, files


def prepare(args):
    require(args.offline_ack, "签名仅在离线机器执行：断网后使用 prepare --offline-ack。")
    require(os.geteuid() != 0, "离线签名请使用普通用户运行，不能使用 sudo。")
    version = "0.9.1"
    sources = {arch: absolute(ask(getattr(args, arch), f"{arch} 发行压缩包路径",
                                f"serverfleet_{version}_linux_{arch}.tar.gz")) for arch in ARCHIVES}
    for arch, path in sources.items():
        require(path.is_file() and file_hash(path) == ARCHIVES[arch], f"{arch} 压缩包与 v0.9.1 官方固定摘要不符。")
    output = absolute(ask(args.output, "新的公开文件包输出目录", "serverfleet-installation-public"))
    key_dir = absolute(ask(args.key_directory, "离线发行密钥目录（不能上传）", "~/serverfleet-offline/keys"))
    key_id = ask(args.key_id, "发行密钥 ID", "release-2026")
    require(re.fullmatch(r"[A-Za-z0-9_-]{1,128}", key_id), "发行密钥 ID 无效。")
    require(not output.exists() and output != key_dir and output not in key_dir.parents and key_dir not in output.parents,
            "输出必须是新的目录，且与离线密钥目录互不包含。")
    installer = absolute(ask(args.installer, "与本脚本同一提交的 install.sh 路径", str(ROOT / "install.sh")))
    require(installer.is_file() and installer.stat().st_size <= 128 << 10, "安装器文件无效。")
    with tempfile.TemporaryDirectory(prefix="sf-offline-") as temp:
        work = pathlib.Path(temp)
        extracted = {}
        for arch, archive in sources.items():
            container = work / arch
            extract(archive, container)
            extracted[arch] = container / f"serverfleet_{version}_linux_{arch}"
            require(extracted[arch].is_dir() and len(list(container.iterdir())) == 1,
                    "发行压缩包顶层目录不符合官方格式。")
            private_tree(extracted[arch])
            manifest, _ = manifest_files(extracted[arch], arch)
            require(manifest["version"] == version and manifest["sequence"] == 2, "不是预期的 v0.9.1 / sequence 2。")
        native_arch = {"x86_64": "amd64", "aarch64": "arm64"}.get(platform.machine())
        require(native_arch is not None, "离线主机需要 Linux amd64/arm64。")
        controller = extracted[native_arch] / "controller"
        if not key_dir.exists():
            key_dir.parent.mkdir(mode=0o700, parents=True, exist_ok=True)
            require(key_dir.parent.stat().st_uid == os.geteuid()
                    and stat.S_IMODE(key_dir.parent.stat().st_mode) == 0o700,
                    "离线密钥目录的父目录须由当前用户所有且为 0700；请使用新的专用父目录。")
            run([controller, "release", "keygen", "-directory", key_dir, "-key-id", key_id, "-offline-ack"])
        require(stat.S_IMODE(key_dir.stat().st_mode) == 0o700 and key_dir.stat().st_uid == os.geteuid(), "离线密钥目录必须由当前用户所有且为 0700。")
        keys = read_json(key_dir / "release-public.json", 8192)
        require(key_id in keys and len(base64.b64decode(keys[key_id], validate=True)) == 32, "密钥 ID 与公开密钥不匹配。")
        output.mkdir(mode=0o700)
        publish = output / "publish"
        publish.mkdir(mode=0o700)
        (publish / version).mkdir(mode=0o700)
        for arch in ARCHIVES:
            run([controller, "release", "package-installation", "-source-directory", extracted[arch],
                 "-output", publish / version / arch, "-key", key_dir / "release-private.key",
                 "-key-id", key_id, "-offline-ack"])
        write_new(publish / "install.sh", installer.read_bytes())
        inventory = {}
        for file in sorted(publish.rglob("*")):
            if file.is_file():
                inventory[file.relative_to(publish).as_posix()] = {"sha256": file_hash(file), "bytes": file.stat().st_size}
        trust = {"format": 1, "version": version, "minimumSequence": 2, "keyId": key_id,
                 "publicKey": keys[key_id], "files": inventory}
        write_json(output / "trust.json", trust)
        verify_bundle(output, file_hash(output / "trust.json"))
        # Export only the allowlisted public tree and metadata, never key_dir.
        archive = output / "installation-public.tar.gz"
        with tarfile.open(archive, "w:gz") as tar:
            tar.add(output / "trust.json", arcname="trust.json", recursive=False)
            for name in sorted(inventory):
                tar.add(publish / name, arcname="publish/" + name, recursive=False)
        print("\n公开文件包：", archive)
        print("请通过独立可信渠道保存下面的指纹；在线配置时输入它：")
        print("trust.json SHA256:", file_hash(output / "trust.json"))
        print("仅上传 installation-public.tar.gz；离线密钥目录留在离线机器。")


def verify_signature(directory, public_key):
    key = base64.b64decode(public_key, validate=True)
    require(len(key) == 32, "发行公钥长度无效。")
    with tempfile.TemporaryDirectory(prefix="sf-verify-") as temp:
        root = pathlib.Path(temp)
        write_new(root / "key.der", bytes.fromhex("302a300506032b6570032100") + key)
        write_new(root / "message", DOMAIN + (directory / "installation.manifest.json").read_bytes())
        signature = directory / "installation.signature"
        require(signature.stat().st_size == 64, "签名长度无效。")
        run(["openssl", "pkeyutl", "-verify", "-pubin", "-keyform", "DER", "-inkey", root / "key.der",
             "-rawin", "-in", root / "message", "-sigfile", signature])


def verify_bundle(root, expected):
    require(re.fullmatch(r"[0-9a-f]{64}", expected or ""), "必须提供离线记录的 trust.json SHA256。")
    require(file_hash(root / "trust.json") == expected, "公开文件包信任指纹不匹配，拒绝继续。")
    trust = read_json(root / "trust.json")
    require(set(trust) == {"format", "version", "minimumSequence", "keyId", "publicKey", "files"}
            and trust["format"] == 1 and re.fullmatch(r"[0-9]+\.[0-9]+\.[0-9]+", trust["version"])
            and type(trust["minimumSequence"]) is int and 0 < trust["minimumSequence"] <= 2**53 - 1
            and re.fullmatch(r"[A-Za-z0-9_-]{1,128}", trust["keyId"]), "信任元数据无效。")
    inventory = trust["files"]
    require(isinstance(inventory, dict) and 13 <= len(inventory) <= 2100, "公开文件数量无效。")
    publish = root / "publish"
    allowed = {"install.sh"}
    for arch in ARCHIVES:
        directory = publish / trust["version"] / arch
        manifest, files = manifest_files(directory, arch)
        require(manifest["version"] == trust["version"] and manifest["sequence"] == trust["minimumSequence"], "签名版本与元数据不一致。")
        verify_signature(directory, trust["publicKey"])
        allowed.update(f"{trust['version']}/{arch}/{name}" for name in [*files, "installation.manifest.json", "installation.signature"])
    require(set(inventory) == allowed, "公开文件包含遗漏或额外的文件。")
    actual = {file.relative_to(root).as_posix() for file in root.rglob("*") if file.is_file()}
    require(actual == {"trust.json", *("publish/" + name for name in allowed)}, "文件包包含未授权文件。")
    for name, entry in inventory.items():
        require(relative_name(name) and set(entry) == {"bytes", "sha256"}, "公开文件清单无效。")
        file = publish / name
        require(file.stat().st_size == entry["bytes"] and file_hash(file) == entry["sha256"], "公开文件摘要不匹配。")
    require(inventory["install.sh"]["bytes"] <= 128 << 10, "安装脚本过大。")
    return trust


def flag_values(argv):
    values, index = {}, 1
    booleans = {"disable-cleanup-retries"}
    while index < len(argv):
        arg = argv[index]
        require(arg.startswith("-") and arg.lstrip("-"), "不是可识别的直接 Controller 启动命令。")
        name, separator, value = arg.lstrip("-").partition("=")
        require(name not in values, "Controller 存在重复启动参数。")
        if not separator:
            if name in booleans:
                value = "true"
            else:
                index += 1
                require(index < len(argv), "Controller 启动参数缺少值。")
                value = argv[index]
        values[name] = value
        index += 1
    return values


def systemd_word(value):
    require(value and not any(ord(char) < 32 or ord(char) == 127 for char in value), "服务参数包含控制字符。")
    return '"' + value.replace("\\", "\\\\").replace('"', '\\"').replace("%", "%%").replace("$", "$$") + '"'


def controller_state(service):
    require(re.fullmatch(r"[A-Za-z0-9_.@-]+\.service", service) and service != SERVICE, "主控服务名无效。")
    data = run(["systemctl", "show", service, "--property=MainPID,User,Group,FragmentPath,ActiveState,RootDirectory,RootImage,DynamicUser,Type"])
    properties = dict(line.split("=", 1) for line in data.decode().splitlines() if "=" in line)
    require(properties.get("ActiveState") == "active" and int(properties.get("MainPID", "0")) > 0, "主控服务需要先处于运行状态。")
    require(not properties.get("RootDirectory") and not properties.get("RootImage") and properties.get("DynamicUser") != "yes", "当前向导不接管容器、chroot 或动态用户服务。")
    require(properties.get("Type") in ("simple", "exec", "notify"), "当前主控服务类型不支持安全追加启动参数。")
    username = properties.get("User") or "root"
    user = pwd.getpwuid(int(username)) if username.isdecimal() else pwd.getpwnam(username)
    require(user.pw_uid != 0, "主控应以固定普通用户运行；请先由部署者完成现有 root 服务迁移。")
    proc = pathlib.Path("/proc") / properties["MainPID"]
    argv = [item.decode() for item in (proc / "cmdline").read_bytes().split(b"\0") if item]
    require(argv and pathlib.Path(argv[0]).is_absolute() and pathlib.Path(argv[0]).resolve() == (proc / "exe").resolve(), "当前主控不是直接 ExecStart；受监督或包装服务请按 QUICK-SETUP.md 集成，不能改写冻结策略。")
    values = flag_values(argv)
    require(not values.get("resident-supervisor") and "db" in values and "origin" in values
            and values.get("agent-pki-mode", "provisioned") == "provisioned", "需要直接运行、已配置离线 Root/在线 Intermediate 的正式 Controller。")
    require("installation-catalog" not in values, "服务已配置 installation-catalog；请检查原目录，向导不覆盖已有配置。")
    for name in ("db", "tls-cert", "tls-key"):
        require(name in values and pathlib.Path(values[name]).is_absolute(), "主控 db/tls-cert/tls-key 必须使用绝对路径。")
    require(re.fullmatch(r"[0-9a-f]{64}", values.get("agent-root-pin", "")), "主控缺少已核对的内部 Root 指纹。")
    return properties, user, argv, values


def origin(value):
    parsed = urllib.parse.urlsplit(value)
    require(parsed.scheme == "https" and parsed.hostname and not parsed.username and not parsed.password
            and parsed.path == "" and not parsed.query and not parsed.fragment
            and value == "https://" + parsed.netloc and not any(char.isspace() for char in value), "必须使用不带路径的 HTTPS Origin。")
    require(re.fullmatch(r"[A-Za-z0-9.-]+", parsed.hostname), "Agent 入口需要 DNS 名称。")
    try:
        ipaddress.ip_address(parsed.hostname)
    except ValueError:
        pass
    else:
        raise SetupError("Agent 入口需要域名，不能使用 IP 字面量。")
    require(parsed.port is None or 0 < parsed.port <= 65535, "HTTPS 端口无效。")
    return parsed


def addresses(value):
    result = []
    for item in value.split(","):
        ip = ipaddress.ip_address(item.strip())
        require(not ip.is_unspecified and not ip.is_multicast and str(ip) not in result, "固定 IP 不得重复、未指定或为组播。")
        result.append(str(ip))
    require(1 <= len(result) <= 8, "固定 IP 数量必须在 1 至 8 之间。")
    return result


def root_certificate(path, pin=None):
    with path.open("rb") as stream:
        raw = stream.read(16385)
    require(len(raw) <= 16384 and raw.count(b"-----BEGIN CERTIFICATE-----") == 1
            and raw.count(b"-----END CERTIFICATE-----") == 1
            and b"PRIVATE KEY" not in raw, "需要单张公开根 CA，不是证书链或私钥。")
    text = run(["openssl", "x509", "-in", path, "-noout", "-text"])
    require(b"CA:TRUE" in text, "根证书必须是 CA。")
    run(["openssl", "verify", "-check_ss_sig", "-CAfile", path, path])
    public = run(["openssl", "x509", "-in", path, "-pubkey", "-noout"])
    spki = run(["openssl", "pkey", "-pubin", "-outform", "DER"], public)
    require(pin is None or digest(spki) == pin, "内部 Root 与主控固定指纹不匹配。")
    return raw.decode()


def pinned_get(url, ips, ca, limit):
    parsed = urllib.parse.urlsplit(url)
    require(parsed.scheme == "https" and parsed.hostname and not parsed.username
            and not parsed.password and not parsed.query and not parsed.fragment,
            "验证 URL 必须为不含凭据、查询或片段的 HTTPS 地址。")
    context = ssl.create_default_context(cadata=ca)
    context.minimum_version = ssl.TLSVersion.TLSv1_3
    last = None
    for ip in ips:
        try:
            connection = http.client.HTTPSConnection(parsed.hostname, parsed.port or 443, timeout=10, context=context)
            connection._create_connection = lambda address, timeout, source_address=None: socket.create_connection((ip, address[1]), timeout)
            try:
                connection.request("GET", parsed.path or "/")
                response = connection.getresponse()
                require(response.status == 200, "HTTPS 验证返回非 200（不跟随跳转）。")
                body = response.read(limit + 1)
                require(len(body) <= limit, "HTTPS 响应过大。")
                return body
            finally:
                connection.close()
        except (OSError, http.client.HTTPException, SetupError) as error:
            last = error
    raise SetupError("固定 IP / TLS1.3 / 证书链验证失败；请核对地址和服务端日志。") from last


def owned_root_parent(path):
    require(path == path.resolve() and not path.exists(), "安装目录必须为不含链接的新绝对路径。")
    for ancestor in path.parents:
        info = ancestor.stat()
        require(info.st_uid == 0 and not info.st_mode & 0o022, "安装目录祖先必须由 root 所有且不可由组/其他用户写入。")


def distribution_unit(base, user, group):
    executable = pathlib.Path(sys.executable).resolve()
    return ("[Unit]\nDescription=Server Fleet public installation distribution\nAfter=network.target\n\n"
            f"[Service]\nType=simple\nUser={user}\nGroup={group}\nUMask=0077\n"
            "ExecStart=" + " ".join(systemd_word(str(word)) for word in [executable, base / SCRIPT, "serve", "--config", base / "server.json"])
            + "\nNoNewPrivileges=yes\nPrivateTmp=yes\nProtectSystem=strict\nProtectHome=read-only\n"
            "CapabilityBoundingSet=\nRestrictSUIDSGID=yes\nRestart=no\n\n[Install]\nWantedBy=multi-user.target\n")


def running_argv(service):
    pid = run(["systemctl", "show", service, "--property=MainPID", "--value"]).decode().strip()
    require(pid.isdecimal() and int(pid) > 0, "主控没有实际运行的 PID。")
    return [item.decode() for item in (pathlib.Path("/proc") / pid / "cmdline").read_bytes().split(b"\0") if item]


def configure(args):
    require(os.geteuid() == 0, "在线配置需要 sudo，以创建 systemd 服务和配置普通用户权限。")
    service = ask(args.service, "现有主控 systemd 服务名", "serverfleet-controller.service")
    properties, user, argv, values = controller_state(service)
    controller = origin(values["origin"])
    authority = values.get("agent-authority")
    require(authority is not None and authority == controller.netloc
            and values.get("agent-server-name", controller.hostname) == controller.hostname,
            "v0.9.1 的 catalog 绑定管理 Origin：须为相同直连 mTLS 域名/端口。分离入口不能自动套用。")
    # Reachability is tested against actual pinned addresses, never environment proxies.
    ips = addresses(ask(args.addresses, "主控可直达的固定 IP（多个用逗号分隔）"))
    require(any(ipaddress.ip_address(ip).version == 4 for ip in ips), "当前分发服务至少需要一个可直达的固定 IPv4。")
    bundle = absolute(ask(args.bundle, "离线签名得到的 installation-public.tar.gz 路径"))
    expected = ask(args.trust_sha256, "离线记录的 trust.json SHA256")
    https_root = absolute(ask(args.https_root, "主控 HTTPS 根 CA PEM 文件路径（不是 fullchain）"))
    ca = root_certificate(https_root)
    pki = absolute(values.get("agent-pki-dir", str(pathlib.Path(values["db"]).parent / "pki")))
    identity_path = pki / "root.pem"
    identity = root_certificate(identity_path, values["agent-root-pin"])
    port = args.port or 9444
    require(1024 <= port <= 65535 and port != (controller.port or 443), "分发端口需在 1024 至 65535 且不同于主控端口。")
    source_origin = "https://" + controller.hostname + ":" + str(port)
    base = absolute(args.directory or "/var/lib/serverfleet-installation")
    owned_root_parent(base)
    unit = SYSTEMD_DIR / SERVICE
    dropdir = SYSTEMD_DIR / (service + ".d")
    dropin = dropdir / "90-serverfleet-installation.conf"
    require(not unit.exists() and not unit.is_symlink() and not dropin.exists() and not dropin.is_symlink(), "已有同名服务或 drop-in，拒绝覆盖。")
    require(not dropdir.is_symlink(), "systemd drop-in 目录不能是符号链接。")
    run(["openssl", "verify", "-CAfile", https_root, "-untrusted", values["tls-cert"], "-verify_hostname", controller.hostname, values["tls-cert"]])
    ready = json.loads(pinned_get(values["origin"] + "/readyz", ips, ca, 8192))
    require(ready.get("status") == "ready", "主控尚未就绪，请先修复 /readyz。")
    # Binding is preflight only; no firewall or reverse proxy is changed.
    with socket.socket() as sock:
        sock.bind(("0.0.0.0", port))
    with tempfile.TemporaryDirectory(prefix="sf-setup-") as temp:
        stage = pathlib.Path(temp) / "bundle"
        extract(bundle, stage)
        trust = verify_bundle(stage, expected)
        downloads = lambda name: {"url": source_origin + "/" + name, "sha256": trust["files"][name]["sha256"]}
        catalog = {"format": 1, "source": {"origin": source_origin, "addresses": ips, "rootPem": ca,
                   "version": trust["version"], "keyId": trust["keyId"], "publicKey": trust["publicKey"],
                   "minimumSequence": trust["minimumSequence"]}, "identityRootPem": identity, "httpsRootPem": ca,
                   "controllerOrigin": values["origin"], "controllerAddresses": ips, "rootPin": values["agent-root-pin"],
                   "installerScript": downloads("install.sh"),
                   "bootstrap": {arch: downloads(trust["version"] + "/" + arch + "/controller") for arch in ARCHIVES}}
        print(f"校验通过：{service}；发行 {trust['version']}；分发入口 {source_origin}")
        if args.check:
            print("CHECK_OK：未修改主控配置或 systemd。")
            return
        require(controller_state(service)[2] == argv, "校验期间主控启动参数发生变化，请重新运行。")
        # Everything below is recorded; failures restore the old startup command.
        changed = []
        try:
            base.mkdir(mode=0o755)
            base.chmod(0o755)
            shutil.copytree(stage / "publish", base / "publish")
            for path in [base / "publish", *(base / "publish").rglob("*")]:
                path.chmod(0o555 if path.is_dir() else 0o444)
            write_new(base / SCRIPT, pathlib.Path(__file__).read_bytes(), 0o555)
            write_json(base / "server.json", {"directory": str(base / "publish"), "files": trust["files"],
                       "certificate": values["tls-cert"], "key": values["tls-key"], "port": port})
            (base / "server.json").chmod(0o644)
            private = base / "controller-config"
            private.mkdir(mode=0o700)
            os.chown(private, user.pw_uid, user.pw_gid)
            write_json(private / "installation-catalog.json", catalog)
            os.chown(private / "installation-catalog.json", user.pw_uid, user.pw_gid)
            group = properties.get("Group") or str(user.pw_gid)
            write_new(unit, distribution_unit(base, user.pw_name, group), 0o644)
            changed.append(unit)
            run(["systemctl", "daemon-reload"])
            run(["systemctl", "start", SERVICE])
            # Prove real HTTPS delivery before modifying the Controller service.
            for attempt in range(10):
                try:
                    raw = pinned_get(source_origin + "/install.sh", ips, ca, 128 << 10)
                    require(digest(raw) == catalog["installerScript"]["sha256"], "分发 HTTPS 内容摘要不匹配。")
                    break
                except SetupError:
                    if attempt == 9:
                        raise
                    time.sleep(1)
            dropdir.mkdir(mode=0o755, exist_ok=True)
            dropdir.chmod(0o755)
            command = argv + ["-installation-catalog", str(private / "installation-catalog.json")]
            write_new(dropin, "[Service]\nExecStart=\nExecStart=" + " ".join(map(systemd_word, command)) + "\n", 0o644)
            changed.append(dropin)
            write_json(base / "receipt.json", {"format": 1, "service": service, "trustSha256": expected,
                       "origin": source_origin, "dropin": str(dropin), "state": "CONFIGURED_NOT_YET_VERIFIED"})
            run(["systemctl", "daemon-reload"])
            run(["systemctl", "restart", service])
            for attempt in range(20):
                try:
                    ready = json.loads(pinned_get(values["origin"] + "/readyz", ips, ca, 8192))
                    require(ready.get("status") == "ready", "主控未就绪。")
                    break
                except (SetupError, json.JSONDecodeError):
                    if attempt == 19:
                        raise SetupError("安装目录启用后主控未恢复就绪。") from None
                    time.sleep(1)
            require(running_argv(service) == command, "实际主控进程未加载新的安装目录参数，拒绝认定配置成功。")
            run(["systemctl", "enable", SERVICE])
            receipt = read_json(base / "receipt.json")
            receipt["state"] = "READY_PENDING_NODE_ACCEPTANCE"
            write_json(base / "receipt.json.next", receipt)
            os.replace(base / "receipt.json.next", base / "receipt.json")
            print("\n配置完成。刷新管理页面，在“添加节点”生成安装命令。")
            print("尚需从目标节点验证分发端口可达；本脚本不改防火墙。")
            print("管理员登录后 /api/v1/installations/catalog 应返回 available:true。")
            print("回退：删除本脚本新增的 drop-in 后 daemon-reload 并重启原主控；停用分发服务。")
            print("drop-in:", dropin)
        except BaseException:
            for path in reversed(changed):
                with contextlib.suppress(OSError):
                    path.unlink()
            subprocess.run(["systemctl", "disable", "--now", SERVICE], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, check=False)
            subprocess.run(["systemctl", "daemon-reload"], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, check=False)
            if dropin in changed:
                subprocess.run(["systemctl", "restart", service], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, check=False)
            print("配置失败，已移除新增启动覆盖并尝试恢复原服务；公开文件与 receipt 保留供检查。", file=sys.stderr)
            raise


class PublicServer(http.server.ThreadingHTTPServer):
    daemon_threads = True
    request_queue_size = 16

    def __init__(self, address, handler):
        self.slots = threading.BoundedSemaphore(16)
        super().__init__(address, handler)

    def process_request(self, request, client_address):
        if not self.slots.acquire(blocking=False):
            self.shutdown_request(request)
            return
        try:
            super().process_request(request, client_address)
        except BaseException:
            self.slots.release()
            raise

    def process_request_thread(self, request, client_address):
        try:
            super().process_request_thread(request, client_address)
        finally:
            self.slots.release()

    def handle_error(self, request, client_address):
        # Invalid TLS peers/timeouts are ordinary network failures; do not emit
        # private filesystem traces or flood journald with handshake details.
        if not isinstance(sys.exc_info()[1], OSError):
            print("PUBLIC_DISTRIBUTION_REQUEST_FAILED", file=sys.stderr, flush=True)


def public_handler(directory, files):
    class Handler(http.server.BaseHTTPRequestHandler):
        server_version = "ServerFleetDistribution/1"
        sys_version = ""

        def setup(self):
            self.request.settimeout(10)
            super().setup()

        def log_message(self, *args):
            pass

        def do_GET(self):
            self.send_file(False)

        def do_HEAD(self):
            self.send_file(True)

        def send_file(self, head):
            name = self.path[1:] if self.path.startswith("/") else ""
            if name not in files:
                self.send_error(404)
                return
            fd = os.open(directory / name, os.O_RDONLY | os.O_NOFOLLOW)
            with os.fdopen(fd, "rb") as stream:
                info = os.fstat(stream.fileno())
                if not stat.S_ISREG(info.st_mode) or info.st_nlink != 1 or info.st_size != files[name]["bytes"]:
                    self.send_error(503)
                    return
                self.send_response(200)
                self.send_header("Content-Type", "application/octet-stream")
                self.send_header("Content-Length", str(info.st_size))
                self.send_header("X-Content-Type-Options", "nosniff")
                self.send_header("Cache-Control", "no-store")
                self.end_headers()
                if not head:
                    shutil.copyfileobj(stream, self.wfile, 1 << 20)
    return Handler


def serve(args):
    require(os.geteuid() != 0, "分发服务必须以普通用户运行。")
    config = read_json(absolute(args.config))
    directory = absolute(config["directory"])
    for name, entry in config["files"].items():
        require(relative_name(name) and file_hash(directory / name) == entry["sha256"], "分发文件启动校验失败。")
    context = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
    context.minimum_version = ssl.TLSVersion.TLSv1_3
    context.load_cert_chain(config["certificate"], config["key"])
    with PublicServer(("0.0.0.0", config["port"]), public_handler(directory, config["files"])) as server:
        server.socket = context.wrap_socket(server.socket, server_side=True, do_handshake_on_connect=False)
        print("PUBLIC_DISTRIBUTION_READY", flush=True)
        server.serve_forever()


def main(argv=None):
    require(sys.platform == "linux", "当前脚本支持 Linux。")
    os.umask(0o077)
    parser = argparse.ArgumentParser(description="Server Fleet 安装向导：为已部署主控启用可信节点一键安装。")
    sub = parser.add_subparsers(dest="command")
    prep = sub.add_parser("prepare", help="离线：验证官方包并生成仅含公开材料的签名包")
    prep.add_argument("--offline-ack", action="store_true")
    for option in ("amd64", "arm64", "output", "key-directory", "key-id", "installer"):
        prep.add_argument("--" + option)
    config = sub.add_parser("configure", help="在线：识别已有主控，部署 HTTPS 分发并启用 catalog")
    for option in ("service", "bundle", "trust-sha256", "addresses", "https-root", "directory"):
        config.add_argument("--" + option)
    config.add_argument("--port", type=int)
    config.add_argument("--check", action="store_true", help="只校验输入，不修改服务")
    server = sub.add_parser("serve", help=argparse.SUPPRESS)
    server.add_argument("--config", required=True)
    args = parser.parse_args(argv)
    if args.command is None:
        print("Server Fleet 安装向导\n1. 为已有主控启用节点一键安装\n2. 在离线机器制作签名公开文件包")
        choice = ask(None, "选择", "1")
        require(choice in ("1", "2"), "无效选项。")
        if choice == "2":
            print("离线机器使用普通用户运行；仅在已经断网时确认。")
            require(ask(None, "输入 OFFLINE 确认本机已断网") == "OFFLINE", "取消签名。")
        return main(["configure"] if choice == "1" else ["prepare", "--offline-ack"])
    if args.command == "prepare":
        return prepare(args)
    if args.command == "serve":
        return serve(args)
    lock_path = "/run/serverfleet-installation-setup.lock"
    require(os.geteuid() == 0, "请使用 sudo 运行在线配置。")
    with os.fdopen(os.open(lock_path, os.O_CREAT | os.O_RDWR | os.O_NOFOLLOW, 0o600), "w") as lock:
        try:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            raise SetupError("另一个安装向导正在运行。") from None
        return configure(args)


if __name__ == "__main__":
    try:
        main()
    except (SetupError, OSError, ValueError, KeyError, TypeError, tarfile.TarError, subprocess.SubprocessError) as error:
        print("安装未完成：" + (str(error) if isinstance(error, SetupError) else type(error).__name__)
              + "。请保留现场，并参阅 deployment/QUICK-SETUP.md。", file=sys.stderr)
        sys.exit(1)

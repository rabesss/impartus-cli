#!/usr/bin/env python3
"""Ingest NotebookLM credentials from environment secrets and verify them.

Bridges the phone-side capture (see phone-cookie-kit.html) and the
``notebooklm`` CLI, so an unattended environment such as a Cursor Cloud agent VM
can authenticate without a browser or an interactive login.

Recognised secrets, in precedence order:

  NOTEBOOKLM_MASTER_TOKEN_JSON  {"email","android_id","master_token"}
      Durable. Re-mints a fresh cookie jar on every ingest, so it survives
      cookie expiry indefinitely. This is the one worth persisting.

  NOTEBOOKLM_BOOTSTRAP_JSON     {"email","oauth_token","android_id"}
      One-shot. Exchanges a single-use EmbeddedSetup oauth_token for a master
      token, then mints cookies. Run ``export-master-token`` afterwards to
      capture the durable credential.

  NOTEBOOKLM_AUTH_JSON          {"cookies":[...]}
      Playwright storage_state. Read directly by the library with no files
      written; expires when Google rotates the session (2-4 weeks).

Secret values are never printed. Diagnostics report names, counts, domains and
expiry dates only. The single exception is ``export-master-token``, which exists
to hand you the credential and says so loudly.
"""

from __future__ import annotations

import argparse
import json
import os
import shutil
import subprocess
import sys
from pathlib import Path

MASTER_TOKEN_ENV = "NOTEBOOKLM_MASTER_TOKEN_JSON"
BOOTSTRAP_ENV = "NOTEBOOKLM_BOOTSTRAP_JSON"
AUTH_JSON_ENV = "NOTEBOOKLM_AUTH_JSON"

# Tier 1 from notebooklm-py's _auth/cookie_policy.py: Google deterministically
# rejects a jar missing either of these.
TIER1_COOKIES = ("SID", "__Secure-1PSIDTS")

INSTALL_HINT = "pip install --pre 'notebooklm-py[headless]>=0.8.0rc1'"


class AuthError(Exception):
    """A recoverable setup problem, reported without a traceback."""


# --------------------------------------------------------------------- helpers


def log(msg: str = "") -> None:
    print(msg, file=sys.stderr, flush=True)


def cli_path() -> str:
    found = shutil.which("notebooklm")
    if not found:
        raise AuthError(
            f"The 'notebooklm' CLI is not on PATH. Install it with:\n    {INSTALL_HINT}"
        )
    return found


def run_cli(args: list[str], *, drop_env_auth: bool = True) -> subprocess.CompletedProcess[str]:
    """Invoke the notebooklm CLI.

    ``NOTEBOOKLM_AUTH_JSON`` activates an env-var fast path that makes the
    library refuse login and import subcommands, so it is removed for any
    command that needs to write to a profile.
    """
    env = dict(os.environ)
    if drop_env_auth:
        env.pop(AUTH_JSON_ENV, None)
    # Fixed argv with no shell, so pasted credentials cannot be reinterpreted.
    return subprocess.run(
        [cli_path(), *args],
        capture_output=True,
        text=True,
        env=env,
        check=False,
    )


def load_secret(name: str, required_keys: tuple[str, ...]) -> dict[str, str]:
    raw = os.environ.get(name, "").strip()
    if not raw:
        raise AuthError(f"{name} is not set.")
    try:
        data = json.loads(raw)
    except json.JSONDecodeError as exc:
        raise AuthError(
            f"{name} is not valid JSON ({exc.msg} at position {exc.pos}). "
            "It must be a single line of JSON, pasted exactly as the phone kit produced it."
        ) from None
    if not isinstance(data, dict):
        raise AuthError(f"{name} must be a JSON object, got {type(data).__name__}.")
    missing = [k for k in required_keys if not str(data.get(k, "")).strip()]
    if missing:
        raise AuthError(f"{name} is missing required field(s): {', '.join(missing)}.")
    return {k: str(v).strip() for k, v in data.items() if isinstance(v, (str, int, float))}


def profile_dir() -> Path:
    """Resolve the active profile directory the CLI writes to."""
    result = run_cli(["auth", "check", "--json"])
    # auth check exits non-zero when unauthenticated but still reports the path.
    for stream in (result.stdout, result.stderr):
        try:
            payload = json.loads(stream)
        except (json.JSONDecodeError, TypeError):
            continue
        path = payload.get("storage_path") or payload.get("details", {}).get("storage_path")
        if path:
            return Path(path).parent
    return Path(os.environ.get("HOME", ".")) / ".notebooklm" / "profiles" / "default"


def write_private_json(path: Path, payload: dict[str, object]) -> None:
    """Write JSON at mode 0600, created restrictive rather than widened later."""
    path.parent.mkdir(parents=True, exist_ok=True)
    tmp = path.parent / f".{path.name}.tmp"
    fd = os.open(tmp, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
    with os.fdopen(fd, "w", encoding="utf-8") as handle:
        json.dump(payload, handle)
    tmp.replace(path)


def fail(result: subprocess.CompletedProcess[str], context: str) -> AuthError:
    detail = (result.stderr or result.stdout or "").strip()
    # The CLI already scrubs secrets from its own messages; keep the tail short
    # so a stack trace does not bury the actionable line.
    tail = "\n".join(detail.splitlines()[-6:]) if detail else "(no output)"
    return AuthError(f"{context} failed (exit {result.returncode}):\n{tail}")


# -------------------------------------------------------------------- commands


def ingest_master_token() -> str:
    secret = load_secret(MASTER_TOKEN_ENV, ("email", "android_id", "master_token"))
    target = profile_dir() / "master_token.json"
    log(f"  writing master_token.json for {secret['email']} -> {target}")
    write_private_json(
        target,
        {
            "version": 1,
            "email": secret["email"],
            "android_id": secret["android_id"],
            "master_token": secret["master_token"],
        },
    )
    log("  minting a fresh cookie jar from the master token")
    result = run_cli(["login", "--master-token-refresh"])
    if result.returncode != 0:
        raise fail(result, "Cookie mint from master token")
    return "master token"


def ingest_bootstrap() -> str:
    secret = load_secret(BOOTSTRAP_ENV, ("email", "oauth_token", "android_id"))
    log(f"  exchanging single-use oauth_token for {secret['email']}")
    result = run_cli(
        [
            "login",
            "--master-token",
            "--account",
            secret["email"],
            "--oauth-token",
            secret["oauth_token"],
            "--android-id",
            secret["android_id"],
            "--force",
        ]
    )
    if result.returncode != 0:
        raise fail(
            result,
            "Master-token bootstrap",
        )
    log("  bootstrap succeeded; run 'export-master-token' to save the durable credential")
    return "bootstrap exchange"


def ingest_cookies() -> str:
    """Normalize a pasted cookie jar through the library's domain allowlist.

    The env-var fast path does no domain filtering, so importing to a profile
    first is what actually drops unrelated Google-product cookies. It also
    validates the jar locally before any network call.
    """
    raw = os.environ.get(AUTH_JSON_ENV, "").strip()
    if not raw:
        raise AuthError(f"{AUTH_JSON_ENV} is not set.")
    try:
        payload = json.loads(raw)
    except json.JSONDecodeError as exc:
        raise AuthError(f"{AUTH_JSON_ENV} is not valid JSON ({exc.msg}).") from None
    if not (isinstance(payload, dict) and isinstance(payload.get("cookies"), list)):
        raise AuthError(
            f"{AUTH_JSON_ENV} must be an object with a \"cookies\" list, "
            'for example {"cookies":[...]}. A bare JSON list is not accepted here.'
        )

    names = {
        c.get("name")
        for c in payload["cookies"]
        if isinstance(c, dict) and c.get("value")
    }
    missing = [c for c in TIER1_COOKIES if c not in names]
    if missing:
        raise AuthError(
            f"Cookie jar is missing required cookie(s): {', '.join(missing)}. "
            "Re-export from a browser signed in to notebooklm.google.com."
        )

    log(f"  importing {len(payload['cookies'])} cookies through the domain allowlist")
    # Cookies travel on stdin rather than argv so they never appear in the
    # process table of a shared host.
    result = subprocess.run(
        [cli_path(), "auth", "import-cookies", "-", "--json"],
        input=json.dumps(payload),
        capture_output=True,
        text=True,
        env={k: v for k, v in os.environ.items() if k != AUTH_JSON_ENV},
        check=False,
    )
    if result.returncode != 0:
        raise fail(result, "Cookie import")
    try:
        kept = json.loads(result.stdout).get("cookie_count")
        log(f"  kept {kept} cookies after filtering")
    except (json.JSONDecodeError, AttributeError):
        pass
    log(
        f"  note: while {AUTH_JSON_ENV} stays set, the library reads it directly and "
        "ignores this filtered profile copy. Unset it to use the filtered jar instead."
    )
    return "session cookies"


def cmd_ingest(_: argparse.Namespace) -> int:
    sources = [
        (MASTER_TOKEN_ENV, ingest_master_token),
        (BOOTSTRAP_ENV, ingest_bootstrap),
        (AUTH_JSON_ENV, ingest_cookies),
    ]
    available = [(name, fn) for name, fn in sources if os.environ.get(name, "").strip()]
    if not available:
        width = max(len(n) for n, _ in sources)
        options = "\n".join(
            f"  {name:<{width}}  {note}"
            for name, note in (
                (MASTER_TOKEN_ENV, "(durable, preferred)"),
                (BOOTSTRAP_ENV, "(one-time exchange)"),
                (AUTH_JSON_ENV, "(session cookies)"),
            )
        )
        raise AuthError(
            "No NotebookLM credential found in the environment. Set one of:\n"
            f"{options}\n"
            "See docs/notebooklm-auth.md for how to produce these on a phone."
        )

    name, handler = available[0]
    if len(available) > 1:
        others = ", ".join(n for n, _ in available[1:])
        log(f"note: {name} takes precedence; also present but unused: {others}")
    log(f"Ingesting credential from {name}")
    used = handler()
    log(f"Ingest complete via {used}.")
    return 0


def cmd_verify(args: argparse.Namespace) -> int:
    cli_args = ["auth", "check", "--json"]
    if not args.offline:
        cli_args += ["--test", "--passive"]
    result = run_cli(cli_args, drop_env_auth=False)

    payload = None
    for stream in (result.stdout, result.stderr):
        try:
            payload = json.loads(stream)
            break
        except (json.JSONDecodeError, TypeError):
            continue
    if payload is None:
        raise fail(result, "auth check")

    checks = payload.get("checks", {})
    details = payload.get("details", {})
    account = payload.get("account") or {}
    master = payload.get("master_token") or {}

    source = details.get("auth_source") or "profile storage_state.json"
    log(f"status        : {payload.get('status')}")
    log(f"auth source   : {source}")
    if source == AUTH_JSON_ENV and (profile_dir() / "storage_state.json").exists():
        log("  (this env var shadows the stored profile; unset it to test the profile)")
    log(f"account       : {account.get('email') or '(not recorded)'}")
    log(f"master token  : {'present' if master.get('present') else 'absent'}")
    log(f"cookies       : {len(details.get('cookies_found') or [])}")
    for domain, names in sorted((details.get("cookies_by_domain") or {}).items()):
        log(f"  {domain:<30} {', '.join(sorted(names))}")
    log(f"network test  : {checks.get('token_fetch')}")
    if details.get("error"):
        log(f"error         : {details['error']}")

    if payload.get("status") != "ok":
        raise AuthError(
            "Authentication is not usable yet. If the network test failed, the session "
            "has expired or was captured incompletely; re-run the phone capture."
        )
    if args.offline:
        log(
            "Structure looks valid. This did not contact Google, so an expired session "
            "would still pass; re-run without --offline to confirm it actually works."
        )
    else:
        log("Authentication verified against the live API.")
    return 0


def cmd_export_master_token(_: argparse.Namespace) -> int:
    path = profile_dir() / "master_token.json"
    if not path.exists():
        raise AuthError(
            f"No master token at {path}. Run 'ingest' with {BOOTSTRAP_ENV} set first."
        )
    record = json.loads(path.read_text(encoding="utf-8"))
    payload = json.dumps(
        {
            "email": record["email"],
            "android_id": record["android_id"],
            "master_token": record["master_token"],
        },
        separators=(",", ":"),
    )
    log("")
    log("=" * 70)
    log(f"Save the line below as the secret {MASTER_TOKEN_ENV}.")
    log("")
    log("It is a full-account credential that keeps working after a password")
    log("change, until you revoke it at myaccount.google.com under 'Your devices'.")
    log("Because it is printed here it also lands in this session's transcript, so")
    log("revoke it when the account is no longer needed.")
    log("=" * 70)
    log("")
    print(payload)
    return 0


def cmd_status(_: argparse.Namespace) -> int:
    log("Credential secrets visible in this environment:")
    for name in (MASTER_TOKEN_ENV, BOOTSTRAP_ENV, AUTH_JSON_ENV):
        raw = os.environ.get(name, "").strip()
        if not raw:
            log(f"  {name:<30} not set")
            continue
        try:
            data = json.loads(raw)
        except json.JSONDecodeError:
            log(f"  {name:<30} set, {len(raw)} chars, INVALID JSON")
            continue
        if isinstance(data, dict) and isinstance(data.get("cookies"), list):
            summary = f"{len(data['cookies'])} cookies"
        elif isinstance(data, dict):
            summary = "fields: " + ", ".join(sorted(data.keys()))
        else:
            summary = f"unexpected JSON {type(data).__name__}"
        log(f"  {name:<30} set, {len(raw)} chars, {summary}")

    log("")
    binary = shutil.which("notebooklm")
    if binary:
        version = run_cli(["--version"], drop_env_auth=False).stdout.strip()
        log(f"notebooklm CLI: {binary} ({version or 'unknown version'})")
    else:
        log(f"notebooklm CLI: NOT INSTALLED - {INSTALL_HINT}")
    return 0


# ------------------------------------------------------------------------ main


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(
        prog="nlm_auth.py",
        description=__doc__,
        formatter_class=argparse.RawDescriptionHelpFormatter,
    )
    sub = parser.add_subparsers(dest="command", required=True)

    sub.add_parser("status", help="Report which credential secrets are present (redacted).")
    sub.add_parser("ingest", help="Materialize the best available credential into the profile.")

    verify = sub.add_parser("verify", help="Validate the credential, by default against the API.")
    verify.add_argument(
        "--offline",
        action="store_true",
        help="Skip the network test and validate structure only.",
    )

    sub.add_parser(
        "export-master-token",
        help="Print the durable master token so it can be stored as a secret.",
    )

    args = parser.parse_args(argv)
    handlers = {
        "status": cmd_status,
        "ingest": cmd_ingest,
        "verify": cmd_verify,
        "export-master-token": cmd_export_master_token,
    }
    try:
        return handlers[args.command](args)
    except AuthError as exc:
        log(f"error: {exc}")
        return 1
    except KeyboardInterrupt:
        return 130


if __name__ == "__main__":
    sys.exit(main())

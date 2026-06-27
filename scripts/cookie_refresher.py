#!/usr/bin/env python3
"""Cookie Refresher for Chaturbate DVR.

Reads current cookies from Supabase, tries to refresh cf_clearance using
Scrapling (Playwright stealth browser with Cloudflare Turnstile solving),
merges with existing sessionid/csrftoken, and writes back to Supabase.

If refresh fails, existing cookies are kept (they usually remain valid).

Usage: python scripts/cookie_refresher.py
Requires .env with SUPABASE_URL, SUPABASE_API_KEY.
"""

import json
import os
import sys
import urllib.error
import urllib.request
from pathlib import Path


def load_dotenv(path=".env"):
    p = Path(path)
    if not p.exists():
        return
    for line in p.read_text().splitlines():
        line = line.strip()
        if not line or line.startswith("#") or "=" not in line:
            continue
        key, _, val = line.partition("=")
        key = key.strip()
        val = val.strip().strip("\"'")
        if key and not os.environ.get(key):
            os.environ[key] = val


def supabase_request(method, url, api_key, data=None):
    body = json.dumps(data).encode() if data else None
    req = urllib.request.Request(url, data=body, method=method)
    req.add_header("apikey", api_key)
    req.add_header("Authorization", f"Bearer {api_key}")
    if body:
        req.add_header("Content-Type", "application/json")
    if method == "PATCH":
        req.add_header("Prefer", "return=representation")
    try:
        with urllib.request.urlopen(req, timeout=30) as resp:
            raw = resp.read()
            return json.loads(raw) if raw else None
    except urllib.error.HTTPError as e:
        error_body = e.read().decode() if e.fp else ""
        print(f"  [WARN] Supabase {method} HTTP {e.code}: {error_body[:300]}")
        return None
    except (urllib.error.URLError, TimeoutError) as e:
        print(f"  [WARN] Supabase {method} failed: {e}")
        return None


def parse_cookies(cookie_str):
    result = {}
    if not cookie_str:
        return result
    for pair in cookie_str.split(";"):
        pair = pair.strip()
        if "=" in pair:
            k, _, v = pair.partition("=")
            result[k.strip()] = v.strip()
    return result


def join_cookies(cookie_dict):
    return "; ".join(f"{k}={v}" for k, v in cookie_dict.items())


def extract_single_cookie(cookie_str, name):
    for pair in cookie_str.split(";"):
        pair = pair.strip()
        if "=" in pair:
            k, _, v = pair.partition("=")
            if k.strip() == name:
                return v.strip()
    return None


def try_refresh_with_scrapling(user_agent, proxy=None):
    """Try to get fresh cookies using Scrapling stealth browser.

    Uses Scrapling's StealthyFetcher (Playwright with anti-detection patches)
    to navigate to chaturbate.com, bypass Cloudflare Turnstile via
    solve_cloudflare=True, then extracts cookies from the Playwright context.

    Falls back to DynamicFetcher if StealthyFetcher is unavailable.

    Returns dict of new cookies, or empty dict on failure.
    """
    try:
        from scrapling.fetchers import StealthyFetcher as _Fetcher
        cls_name = "StealthyFetcher"
    except ImportError:
        try:
            from scrapling.fetchers import DynamicFetcher as _Fetcher
            cls_name = "DynamicFetcher"
        except ImportError:
            print("  [INFO] Scrapling not available (pip install 'scrapling[fetchers]')")
            return {}

    print(f"  Using {cls_name} (Playwright-based stealth browser)...")

    for mode_name, mode_proxy in [("direct", None), ("proxy", proxy)]:
        if mode_name == "proxy" and not proxy:
            continue
        print(f"  [{mode_name}] Connecting via {mode_proxy or 'direct'}...")

        pw_proxy = None
        if mode_proxy:
            pw_proxy = mode_proxy.replace("socks5h://", "socks5://", 1)

        try:
            with _Fetcher(
                headless=True,
                solve_cloudflare=True,
                proxy=pw_proxy,
            ) as fetcher:
                session = fetcher.session
                print(f"  [{mode_name}] Browser launched, navigating to chaturbate.com...")

                resp = session.fetch("https://chaturbate.com", timeout=90)
                status = getattr(resp, "status", "?")
                print(f"  [{mode_name}] Page status: {status}")

                body = getattr(resp, "text", "") or ""
                if "verify your age" in body.lower():
                    print(f"  [{mode_name}] Age verification detected")
                    continue

                context = getattr(session, "context", None)
                if context is not None:
                    try:
                        pw_cookies = context.cookies()
                        print(f"  [{mode_name}] Got {len(pw_cookies)} cookies from Playwright")

                        new_cookies = {}
                        for c in pw_cookies:
                            name = c.get("name", "")
                            value = c.get("value", "")
                            if name and value:
                                new_cookies[name] = value
                                if name in ("cf_clearance", "sessionid", "csrftoken", "__cf_bm"):
                                    print(f"  [{mode_name}] {name}=...{value[-20:]}")

                        if "cf_clearance" in new_cookies:
                            print(f"  [{mode_name}] Got cf_clearance!")
                            return new_cookies
                        if new_cookies:
                            print(f"  [{mode_name}] Got {len(new_cookies)} cookies but no cf_clearance")
                            return new_cookies
                    except Exception as e:
                        print(f"  [{mode_name}] Cookie extraction error: {e}")
                else:
                    print(f"  [{mode_name}] No Playwright context available")
        except Exception as e:
            print(f"  [{mode_name}] Scrapling failed: {e}")
            continue

    print("  [INFO] No cookies from Scrapling (both direct and proxy failed)")
    return {}


def save_to_supabase(rest, api_key, value, settings_key="dvr_settings", is_seed=False):
    patch_url = f"{rest}/app_settings?key=eq.{settings_key}"
    result = supabase_request("PATCH", patch_url, api_key, {"value": value})

    if result is not None and result != []:
        label = "seeded" if is_seed else "saved"
        print(f"  [OK] Cookies {label} to Supabase")
        return

    label = "seed" if is_seed else "save"
    print(f"  Row may not exist, trying INSERT for {label}...")
    result = supabase_request(
        "POST",
        f"{rest}/app_settings",
        api_key,
        {"key": settings_key, "value": value},
    )
    if result is not None:
        print(f"  [OK] Cookies {label}d into Supabase")
        return

    # POST may have failed due to transient error — retry PATCH once
    print(f"  POST failed, retrying PATCH...")
    result = supabase_request("PATCH", patch_url, api_key, {"value": value})
    if result is not None and result != []:
        print(f"  [OK] Cookies {label} to Supabase (PATCH retry)")
        return

    print(f"  [WARN] Failed to {label} cookies to Supabase (will retry next run)")


def main():
    print("=" * 50)
    print("  Cookie Refresher")
    print("=" * 50)

    load_dotenv(".env")

    supabase_url = os.environ.get("SUPABASE_URL", "").rstrip("/")
    supabase_key = os.environ.get("SUPABASE_API_KEY", "")
    proxy = os.environ.get("ALL_PROXY", "")
    # Detect node ID matching Go's detectNodeID() logic:
    # 1. NODE_ID env var, 2. GITHUB_REPOSITORY (split by "-", last part),
    # 3. hostname, 4. random fallback
    node_id = os.environ.get("NODE_ID", "")
    if not node_id:
        repo = os.environ.get("GITHUB_REPOSITORY", "")
        if repo:
            parts = repo.split("-")
            node_id = parts[-1] if len(parts) > 1 else repo.replace("/", "-")
        else:
            node_id = os.environ.get("COMPUTERNAME", "unknown")

    if not supabase_url or not supabase_key:
        print("  [SKIP] SUPABASE_URL or SUPABASE_API_KEY not set")
        return

    # Per-node cookie keys prevent cf_clearance IP-binding issues across nodes
    settings_key = f"dvr_settings_{node_id}"
    print(f"  Node ID: {node_id}")
    print(f"  Settings key: {settings_key}")

    rest = f"{supabase_url}/rest/v1"
    get_url = f"{rest}/app_settings?key=eq.{settings_key}&select=value"

    # --- Load current cookies from Supabase ---
    print("\n[1/3] Loading current cookies from Supabase...")
    settings = supabase_request("GET", get_url, supabase_key)

    cookie_str = ""
    user_agent = os.environ.get("USER_AGENT", "")

    if settings and len(settings) > 0:
        val = settings[0].get("value", {})
        cookie_str = val.get("cookies", "")
        if not user_agent:
            user_agent = val.get("user_agent", "")

    # --- If no cookies in Supabase, seed from .env ---
    if not cookie_str:
        env_cookies = os.environ.get("COOKIES", "")
        if env_cookies:
            print("  No cookies in Supabase — seeding from .env...")
            cookie_str = env_cookies
            if not user_agent:
                user_agent = os.environ.get(
                    "USER_AGENT",
                    "Mozilla/5.0 (Windows NT 10.0; Win64; x64) "
                    "AppleWebKit/537.36 (KHTML, like Gecko) "
                    "Chrome/146.0.0.0 Safari/537.36",
                )
            seed_value = {
                "cookies": cookie_str,
                "user_agent": user_agent,
            }
            for key in ("sessionid", "csrftoken", "cf_clearance"):
                val = extract_single_cookie(cookie_str, key)
                if val:
                    seed_value[key] = val
            save_to_supabase(rest, supabase_key, seed_value, settings_key=settings_key, is_seed=True)
        else:
            print("  [SKIP] No cookies found in Supabase or .env")
            return

    if not user_agent:
        user_agent = (
            "Mozilla/5.0 (Windows NT 10.0; Win64; x64) "
            "AppleWebKit/537.36 (KHTML, like Gecko) "
            "Chrome/146.0.0.0 Safari/537.36"
        )
    print(f"  Resolved User-Agent: {user_agent}")
    print(f"UA_EXTRACTED={user_agent}")

    old = parse_cookies(cookie_str)
    print(f"  Loaded {len(old)} cookies")
    print(f"  sessionid: {'[OK]' if 'sessionid' in old else '[NO]'}")
    print(f"  csrftoken: {'[OK]' if 'csrftoken' in old else '[NO]'}")
    print(f"  cf_clearance: {'[OK]' if 'cf_clearance' in old else '[NO]'}")
    print(f"  Proxy: {'[OK] ' + proxy if proxy else '[NO] (direct)'}")

    # --- Try to refresh cookies ---
    print("\n[2/3] Refreshing cookies...")

    new_cookies = try_refresh_with_scrapling(user_agent, proxy)

    # --- Merge and save ---
    print("\n[3/3] Merging cookies...")

    merged = dict(old)
    refreshed = False

    if new_cookies:
        for key in ("cf_clearance", "__cf_bm", "__cfruid", "sessionid", "csrftoken"):
            if key in new_cookies and new_cookies[key]:
                old_val = merged.get(key, "")
                new_val = new_cookies[key]
                if new_val != old_val:
                    merged[key] = new_val
                    refreshed = True

        old_cf = old.get("cf_clearance", "")
        new_cf = merged.get("cf_clearance", "")
        if new_cf and new_cf != old_cf:
            print(f"  cf_clearance refreshed: ...{new_cf[-20:]}")
        elif new_cf:
            print(f"  cf_clearance unchanged (still valid)")
        else:
            print(f"  [WARN] No new cf_clearance from Scrapling — refresh may have been blocked")
    else:
        print("  [INFO] Cookie refresh failed — keeping existing cf_clearance")
        # Keep the existing cf_clearance even if stale. Without ANY clearance
        # the DVR cannot bootstrap a new one because ALL proxy requests get
        # Cloudflare-challenged before they reach the Set-Cookie handler.
        # A stale clearance from a different proxy IP is still better than
        # none — the httpcloak transport will rotate on challenge, and when
        # it finds a clean proxy Cloudflare will issue a fresh clearance.

    print(f"  Total cookies: {len(merged)}")
    print(f"  sessionid: {'[OK]' if 'sessionid' in merged else '[NO]'}")
    print(f"  csrftoken: {'[OK]' if 'csrftoken' in merged else '[NO]'}")
    print(f"  cf_clearance: {'[OK]' if 'cf_clearance' in merged else '[NO]'}")

    new_cookie_str = join_cookies(merged)

    # --- Write back to Supabase (only on actual change) ---
    if refreshed:
        print("\nSaving refreshed cookies to Supabase...")
        settings_value = {
            "cookies": new_cookie_str,
            "user_agent": user_agent,
        }
        for key in ("sessionid", "csrftoken", "cf_clearance"):
            if key in merged:
                settings_value[key] = merged[key]
        save_to_supabase(rest, supabase_key, settings_value, settings_key=settings_key)
        print("\n[OK] Cookies refreshed successfully")
    else:
        # Still save if we removed stale cf_clearance even without a fresh one
        old_cf = old.get("cf_clearance", "")
        new_cf = merged.get("cf_clearance", "")
        if old_cf != new_cf or new_cookie_str != cookie_str:
            print("\nSaving cleaned cookies to Supabase (removed stale cf_clearance)...")
            settings_value = {
                "cookies": new_cookie_str,
                "user_agent": user_agent,
            }
            for key in ("sessionid", "csrftoken", "cf_clearance"):
                if key in merged:
                    settings_value[key] = merged[key]
            save_to_supabase(rest, supabase_key, settings_value, settings_key=settings_key)
            print("\n[OK] Stale cf_clearance removed")
        else:
            print("\n[SKIP] No changes — keeping existing cookies in Supabase")


if __name__ == "__main__":
    main()

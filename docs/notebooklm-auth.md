# NotebookLM Authentication From a Phone

<!-- START doctoc generated TOC please keep comment here to allow auto update -->
**Table of Contents**  *generated automatically*

<!---toc start-->

* [NotebookLM Authentication From a Phone](#notebooklm-authentication-from-a-phone)
  * [Why a phone alone cannot just copy a cookie](#why-a-phone-alone-cannot-just-copy-a-cookie)
  * [Pick a browser that supports extensions](#pick-a-browser-that-supports-extensions)
  * [Convert what you exported into a secret](#convert-what-you-exported-into-a-secret)
  * [The two credential paths](#the-two-credential-paths)
    * [Path A: session cookies (quick, expires in a few weeks)](#path-a-session-cookies-quick-expires-in-a-few-weeks)
    * [Path B: master token (durable, one-time setup)](#path-b-master-token-durable-one-time-setup)
  * [Secret reference](#secret-reference)
  * [Using the credential in the environment](#using-the-credential-in-the-environment)
  * [Troubleshooting](#troubleshooting)
  * [Related](#related)

<!---toc end-->
<!-- END doctoc generated TOC please keep comment here to allow auto update -->

How to authenticate a headless environment (a Cursor Cloud agent VM, CI, or a
server) against Google NotebookLM when the only device you have is a phone.

This is a prerequisite for the automated lecture-upload pipeline: NotebookLM has
no public API for consumer accounts, so the tooling drives Google's internal
endpoints using a real signed-in session. That session has to start with a human
login somewhere, and this document makes that "somewhere" your phone.

## Why a phone alone cannot just copy a cookie

The cookies that carry a Google session are `HttpOnly`. That flag exists
specifically to make them unreadable from page JavaScript, so none of the
following can work, no matter how they are packaged:

- a bookmarklet or `javascript:` URL reading `document.cookie`
- an iOS Shortcut running "Run JavaScript on Web Page"
- copying the request from a normal mobile browser

`document.cookie` can only see `SAPISID` and `APISID`, and those are not enough
to authenticate on their own. The only in-browser mechanism that can read an
`HttpOnly` cookie is the WebExtension `cookies` API, which means **you need a
mobile browser that can install an extension**. That is the one unavoidable
piece of setup.

## Pick a browser that supports extensions

Install one of these alongside your normal browser, then install a single
cookie-export extension from a known source (do not search the store and pick
an arbitrary first hit — exporters read full Google session cookies):

- **Recommended extension** &mdash; [Cookie-Editor](https://cookie-editor.com/)
  (`cookie-editor` by Moustachauve) from the Firefox Add-ons site or Chrome Web
  Store listing that matches your browser. Prefer a temporary browser profile
  for the capture, remove the extension after export, and revoke unused sessions
  at [myaccount.google.com/device-activity](https://myaccount.google.com/device-activity)
  when using a throwaway Google account.
- **Android, recommended** &mdash; Firefox for Android. Arbitrary add-ons install
  via the custom add-on collection flow (create a collection on
  addons.mozilla.org, then enable the hidden debug menu by tapping the Firefox
  logo in Settings five times).
- **Android, simpler** &mdash; Quetta. A Chromium browser that installs Chrome Web
  Store extensions directly, with no developer mode. Currently the most
  frictionless option since Kiwi Browser was discontinued in early 2025.
- **iOS** &mdash; Orion by Kagi. The only iOS browser implementing WebExtension
  APIs; it installs Chrome and Firefox extensions. Extension support is beta, so
  confirm Cookie-Editor can actually list cookies before relying on it.
  Safari's own extensions cannot be side-loaded without a Mac and a developer
  account, so Safari is not an option.

## Convert what you exported into a secret

Raw extension exports are the wrong shape and contain far more than NotebookLM
needs, so run them through the offline converter:

[`scripts/notebooklm-auth/phone-cookie-kit.html`](../scripts/notebooklm-auth/phone-cookie-kit.html)

Download that one file to your phone and open it from Files or Downloads. It
works offline and makes **zero** network requests &mdash; not as a promise, but
because its `Content-Security-Policy` is `default-src 'none'`, which means the
browser itself refuses to let the page open a connection. Nothing you paste can
leave the device except through your own clipboard.

The converter accepts a bare JSON cookie list, a Playwright `storage_state`
object, or a Netscape `cookies.txt` file; merges several pastes; drops cookies
for unrelated Google products; checks that the cookies Google actually requires
are present; and emits a single-line secret value with a copy button. It shows
cookie names, domains, and expiry dates, never values.

## The two credential paths

Choose based on how long you need it to last.

### Path A: session cookies (quick, expires in a few weeks)

Best for getting the agent testing immediately.

1. In your extension-capable browser, sign in at `notebooklm.google.com` and let
   the page load completely. This step is what populates `OSID`, so do not skip it.
2. Export cookies for that site.
3. Repeat for `google.com` and `accounts.google.com`.
4. Paste each export into the converter's **Session cookies** tab.
5. Copy the generated value into a Cursor secret named `NOTEBOOKLM_AUTH_JSON`.

Google rotates these cookies, so redo it when authentication starts failing.

### Path B: master token (durable, one-time setup)

A master token re-mints fresh cookies on demand with no browser, so it survives
cookie expiry indefinitely. This is what makes unattended operation possible.

1. In the same browser, open `https://accounts.google.com/EmbeddedSetup`.
2. Sign in and accept the prompt. The page then sits on a spinner forever &mdash;
   that is expected and does not mean it failed.
3. With the cookie extension, copy the value of the `oauth_token` cookie on
   `accounts.google.com`. It begins with `oauth2_4/`.
4. Paste it plus the account email into the converter's **Master token** tab.
5. Copy the generated value into a Cursor secret named
   `NOTEBOOKLM_BOOTSTRAP_JSON`, and tell the agent promptly &mdash; the token is
   single-use and expires within minutes.
6. The agent exchanges it for the durable credential and can print it back via
   `export-master-token` so you can store it as `NOTEBOOKLM_MASTER_TOKEN_JSON`
   for all future runs.

> **Use a dedicated throwaway Google account for Path B.** A master token grants
> full access to the account, is not limited to NotebookLM, and keeps working
> after a password change until you explicitly revoke it under
> myaccount.google.com → Security → Your devices. Treat it like a password
> you cannot rotate. If the agent prints it, it also lands in that session's
> transcript.

## Secret reference

| Secret | Shape | Lifetime |
| --- | --- | --- |
| `NOTEBOOKLM_MASTER_TOKEN_JSON` | `{"email","android_id","master_token"}` | Until revoked |
| `NOTEBOOKLM_BOOTSTRAP_JSON` | `{"email","oauth_token","android_id"}` | Minutes, single use |
| `NOTEBOOKLM_AUTH_JSON` | `{"cookies":[...]}` | 2&ndash;4 weeks |

Add them in the Cursor Dashboard under Cloud Agents → Secrets. They are injected
as environment variables into new agent VMs. Precedence is top to bottom, so a
master token wins over a bootstrap token, which wins over raw cookies.

## Using the credential in the environment

```bash
pip install --pre 'notebooklm-py[headless]==0.8.0rc1'

python3 scripts/notebooklm-auth/nlm_auth.py status    # which secrets are visible
python3 scripts/notebooklm-auth/nlm_auth.py ingest    # activate the best one
python3 scripts/notebooklm-auth/nlm_auth.py verify    # confirm against the live API
```

`ingest` picks the highest-precedence secret available, writes any credential
files at mode `0600`, and mints a cookie jar when given a master token. `verify`
exercises the real API; add `--offline` to check structure only. After a
bootstrap exchange, `export-master-token` prints the durable credential.

No secret values are printed by `status`, `ingest`, or `verify` &mdash; they
report names, counts, domains, and expiry dates only. `export-master-token` is
the deliberate exception and warns before printing.

The `0.8.0rc1` prerelease pin is required, not incidental: the released `0.7.3`
has neither `login --master-token` nor `auth import-cookies`, so neither path in
this document works on it.

## Troubleshooting

**"Missing required cookies: SID"** &mdash; you are not signed in to Google in
that browser. Sign in and export again.

**"Missing required cookies: `__Secure-1PSIDTS`"** &mdash; open
`notebooklm.google.com`, let it load fully, reload once so Google refreshes the
cookie, then export again.

**"Missing the secondary binding"** &mdash; the export lacks `OSID` and does not
have both `APISID` and `SAPISID`. Export the `notebooklm.google.com` site
cookies specifically; `OSID` is set there rather than on `google.com`.

**"exchange_token rejected the oauth_token"** &mdash; the token is single-use and
short-lived. Redo the EmbeddedSetup steps and pass the fresh value quickly.

**Cookies work for minutes, then stop** &mdash; Google's Device Bound Session
Credentials tie some sessions to the originating device's hardware key, which
makes exported cookies unusable elsewhere. Observed as of 2026-07-29: rollout is
partial and typically does not apply to Firefox or mobile-captured sessions.
Re-check by exporting again and running `nlm_auth.py verify` after a few minutes.
If DBSC affects your account, Path A cannot work and Path B is the only option,
since a master token re-mints cookies server-side instead of replaying captured
ones.

**Redirected to `?location=unsupported`** &mdash; Google is refusing the request
based on the server's IP, not the credential. Re-authenticating will not help.
This affects datacenter and VPN addresses. Observed as of 2026-07-29: Cursor Cloud
egress reached NotebookLM normally in this project's tests — re-check with
`nlm_auth.py verify` before assuming a credential problem on a new host.

## Related

- [`architecture.md`](architecture.md) &mdash; how the downloader pipeline fits together
- [`runbooks.md`](runbooks.md) &mdash; operational troubleshooting

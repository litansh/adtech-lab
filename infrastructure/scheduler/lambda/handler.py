"""Fire a GitHub workflow, on time.

GitHub's own cron could not do it. Over two days its scheduled runs for this
repository were 5-6 hours late and then did not fire at all -- every workflow,
all six, silent past every slot. Scheduled workflows on a private repository are
queued at low priority, and the documentation says so: "delayed or not run".

That is fine for a nightly report. It is not fine for a briefing whose entire
value is arriving at a known time, and it is unfixable from inside GitHub: the
previous attempt scheduled eight hourly slots so that a late one would still
land, and the failure mode was that none of them ran.

So the clock moves outside. EventBridge Scheduler fires at 09:00 Asia/Jerusalem
and this posts a workflow_dispatch. Two things fall out of that:

  - It is punctual. EventBridge is a scheduler rather than a best effort.
  - The DST hack disappears. EventBridge understands named timezones, so
    "09:00 in Israel" is one line instead of two UTC slots and a shell gate.
"""
import json
import os
import urllib.error
import urllib.request

import boto3

API = "https://api.github.com/repos/{repo}/actions/workflows/{wf}/dispatches"

_secrets = boto3.client("secretsmanager")
_cached = None


def _token():
    """Read the token once per container. It is never logged: a scheduler that
    prints its own credentials on failure is worse than one that does not run."""
    global _cached
    if _cached is None:
        arn = os.environ["TOKEN_SECRET_ARN"]
        _cached = _secrets.get_secret_value(SecretId=arn)["SecretString"].strip()
    return _cached


def handler(event, _context):
    repo = os.environ["REPO"]
    workflow = (event or {}).get("workflow") or os.environ.get("WORKFLOW", "orchestrator.yml")
    ref = os.environ.get("REF", "main")

    req = urllib.request.Request(
        API.format(repo=repo, wf=workflow),
        data=json.dumps({"ref": ref}).encode(),
        headers={
            "Authorization": "Bearer " + _token(),
            "Accept": "application/vnd.github+json",
            "X-GitHub-Api-Version": "2022-11-28",
            "User-Agent": "adtech-lab-scheduler",
            "Content-Type": "application/json",
        },
        method="POST",
    )
    try:
        with urllib.request.urlopen(req, timeout=15) as r:
            # 204 is success for this endpoint.
            print(f"dispatched {workflow} on {ref}: HTTP {r.status}")
            return {"ok": True, "status": r.status, "workflow": workflow}
    except urllib.error.HTTPError as e:
        # Raised, not swallowed. A scheduler that fails quietly is the exact
        # thing being replaced here -- the alarm on this function's errors is
        # what makes a missed morning visible rather than merely absent.
        body = e.read().decode()[:200]
        print(f"dispatch FAILED {workflow}: HTTP {e.code} {body}")
        raise

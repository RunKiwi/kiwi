"""Python client for Kiwi.

Submits to /api/v1/planner/plan — the same path ``kiwi submit`` uses, and the
one that feeds the daemon lease queue. The older /tasks endpoint runs the loop
control-plane-side, never hands work to a daemon, and gates on
``org.CanRun()``, which returns 402 for every free-tier org.
"""

import os
from typing import Any, Dict, List, Optional

import requests

__all__ = ["KiwiClient", "KiwiError"]

DEFAULT_TIMEOUT = 30.0


class KiwiError(Exception):
    """A non-2xx response from the Control Plane."""

    def __init__(self, message: str, status: int, body: Any = None):
        super().__init__(message)
        self.status = status
        self.body = body


class KiwiClient:
    def __init__(self, server: str, token: Optional[str] = None, timeout: float = DEFAULT_TIMEOUT):
        if not server:
            raise ValueError("server is required")
        if server.startswith("http://") and "localhost" not in server and "127.0.0.1" not in server:
            raise ValueError("Refusing to send token over cleartext HTTP. Use HTTPS.")
        self.server = server.rstrip("/")
        self.token = token or os.environ.get("KIWI_SERVER_TOKEN")
        self.timeout = timeout

    def _request(
        self,
        method: str,
        path: str,
        json_body: Optional[Dict[str, Any]] = None,
        idempotency_key: Optional[str] = None,
    ) -> Any:
        headers = {"Authorization": f"Bearer {self.token}"}
        if idempotency_key:
            headers["Idempotency-Key"] = idempotency_key

        response = requests.request(
            method,
            f"{self.server}{path}",
            headers=headers,
            json=json_body,
            timeout=self.timeout,
        )

        try:
            parsed = response.json() if response.content else None
        except ValueError:
            parsed = response.text

        if not response.ok:
            # The message carries the server's status but never the request
            # headers, so a raised error cannot leak the token into a log.
            raise KiwiError(
                f"Kiwi {method} {path} failed: {response.status_code}",
                response.status_code,
                parsed,
            )
        return parsed

    def submit_task(
        self,
        task: str,
        repo_url: Optional[str] = None,
        ref: Optional[str] = None,
        file: Optional[str] = None,
        files: Optional[List[str]] = None,
        test_cmd: Optional[str] = None,
        model: Optional[str] = None,
        max_workers: Optional[int] = None,
        idempotency_key: Optional[str] = None,
    ) -> Dict[str, Any]:
        """Plan and enqueue a task.

        Returns once the plan is accepted — workers run asynchronously, so poll
        :meth:`get_job` for the result.
        """
        if not task:
            raise ValueError("task is required")

        body = {
            "task": task,
            "repo_url": repo_url,
            "ref": ref,
            "file": file,
            "files": files,
            "test_cmd": test_cmd,
            "model": model,
            "max_workers": max_workers,
        }
        body = {k: v for k, v in body.items() if v is not None}

        return self._request(
            "POST", "/api/v1/planner/plan", json_body=body, idempotency_key=idempotency_key
        )

    def get_job(self, job_id: str) -> Dict[str, Any]:
        """Fetch a job and its per-worker task states."""
        if not job_id:
            raise ValueError("job_id is required")
        return self._request("GET", f"/api/v1/jobs/{job_id}")

    def list_jobs(self) -> Dict[str, Any]:
        """List jobs visible to the authenticated org."""
        return self._request("GET", "/api/v1/jobs")

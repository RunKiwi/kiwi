"""Tests for the Kiwi Python SDK, against a stub HTTP server."""

import json
import threading
import unittest
from http.server import BaseHTTPRequestHandler, HTTPServer

from kiwi import KiwiClient, KiwiError

SEEN = {}
RESPONSE = {"status": 202, "body": {"job_id": "job_1"}}


class _Handler(BaseHTTPRequestHandler):
    def _capture(self):
        length = int(self.headers.get("Content-Length") or 0)
        raw = self.rfile.read(length) if length else b""
        SEEN["path"] = self.path
        SEEN["method"] = self.command
        SEEN["headers"] = {k.lower(): v for k, v in self.headers.items()}
        SEEN["body"] = json.loads(raw) if raw else None

        payload = json.dumps(RESPONSE["body"]).encode()
        self.send_response(RESPONSE["status"])
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(payload)))
        self.end_headers()
        self.wfile.write(payload)

    do_GET = _capture
    do_POST = _capture

    def log_message(self, *args):  # keep test output clean
        pass


class SDKTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.server = HTTPServer(("127.0.0.1", 0), _Handler)
        cls.thread = threading.Thread(target=cls.server.serve_forever, daemon=True)
        cls.thread.start()
        cls.base = f"http://127.0.0.1:{cls.server.server_address[1]}"

    @classmethod
    def tearDownClass(cls):
        cls.server.shutdown()

    def setUp(self):
        SEEN.clear()
        RESPONSE["status"] = 202
        RESPONSE["body"] = {"job_id": "job_1"}

    def client(self, token="t"):
        return KiwiClient(self.base, token)

    # The bug this guards: /tasks runs the loop control-plane-side and gates on
    # org.CanRun(), so it returns 402 for every free-tier org.
    def test_submit_posts_to_planner_path(self):
        result = self.client().submit_task("fix Divide()")
        self.assertEqual(SEEN["path"], "/api/v1/planner/plan")
        self.assertEqual(SEEN["method"], "POST")
        self.assertEqual(result["job_id"], "job_1")

    def test_arguments_map_to_wire_format(self):
        self.client().submit_task(
            task="add logging",
            repo_url="https://github.com/x/y",
            ref="main",
            test_cmd="go test ./...",
            max_workers=3,
        )
        self.assertEqual(
            SEEN["body"],
            {
                "task": "add logging",
                "repo_url": "https://github.com/x/y",
                "ref": "main",
                "test_cmd": "go test ./...",
                "max_workers": 3,
            },
        )

    def test_unset_arguments_are_omitted(self):
        self.client().submit_task("only a task")
        self.assertEqual(list(SEEN["body"].keys()), ["task"])

    def test_idempotency_key_forwarded(self):
        self.client().submit_task("t", idempotency_key="abc-123")
        self.assertEqual(SEEN["headers"]["idempotency-key"], "abc-123")

    def test_bearer_token_sent(self):
        self.client("secret-token").submit_task("t")
        self.assertEqual(SEEN["headers"]["authorization"], "Bearer secret-token")

    def test_get_job(self):
        RESPONSE["status"] = 200
        RESPONSE["body"] = {"job_id": "job_1", "tasks": [{"id": "t1", "status": "SUCCEEDED"}]}
        job = self.client().get_job("job_1")
        self.assertEqual(SEEN["path"], "/api/v1/jobs/job_1")
        self.assertEqual(job["tasks"][0]["status"], "SUCCEEDED")

    def test_error_carries_status_and_never_the_token(self):
        RESPONSE["status"] = 402
        RESPONSE["body"] = {"error": "payment required"}
        with self.assertRaises(KiwiError) as ctx:
            self.client("secret-token").submit_task("t")
        self.assertEqual(ctx.exception.status, 402)
        self.assertEqual(ctx.exception.body, {"error": "payment required"})
        self.assertNotIn("secret-token", str(ctx.exception))
        self.assertNotIn("secret-token", json.dumps(ctx.exception.body))

    def test_refuses_cleartext_http_to_remote_host(self):
        with self.assertRaises(ValueError):
            KiwiClient("http://api.example.com", "t")
        KiwiClient("http://localhost:8080", "t")
        KiwiClient("https://api.example.com", "t")

    def test_task_required(self):
        with self.assertRaises(ValueError):
            self.client().submit_task("")

    def test_trailing_slash_does_not_double_up(self):
        KiwiClient(f"{self.base}/", "t").submit_task("t")
        self.assertEqual(SEEN["path"], "/api/v1/planner/plan")


if __name__ == "__main__":
    unittest.main()

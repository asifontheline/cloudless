"""SDK conformance tests (L4, #87): the Python SDK against a live node.

Requires CLOUDLESS_TEST_ADDR / CLOUDLESS_TEST_KEY (set by
sdk/testutil/run_conformance.sh, which starts a real cloudless binary
against a stub backend). Skips itself when no live node is configured, so
plain `pytest`/`unittest` runs elsewhere stay green.
"""
import os
import unittest

import sys
sys.path.insert(0, os.path.join(os.path.dirname(__file__), ".."))
from cloudless import Client, CloudlessError  # noqa: E402

ADDR = os.environ.get("CLOUDLESS_TEST_ADDR")
KEY = os.environ.get("CLOUDLESS_TEST_KEY")


@unittest.skipUnless(ADDR and KEY, "set CLOUDLESS_TEST_ADDR/CLOUDLESS_TEST_KEY to run against a live node")
class TestPythonSDKConformance(unittest.TestCase):
    def setUp(self):
        self.mesh = Client(ADDR, api_key=KEY)

    def test_chat_returns_stub_content(self):
        self.assertEqual(self.mesh.chat("hi"), "hello from the stub backend")

    def test_chat_accepts_message_list(self):
        out = self.mesh.chat([{"role": "user", "content": "hi"}])
        self.assertEqual(out, "hello from the stub backend")

    def test_completions_raw_shape(self):
        out = self.mesh.completions("hi")
        self.assertIn("choices", out)
        self.assertEqual(out["usage"]["prompt_tokens"], 3)

    def test_status_reports_the_backend(self):
        st = self.mesh.status()
        names = [b["Backend"]["name"] for b in st["backends"]]
        self.assertIn("stub", names)

    def test_wrong_key_raises_cloudless_error(self):
        bad = Client(ADDR, api_key="wrong-key")
        with self.assertRaises(CloudlessError) as cm:
            bad.chat("hi")
        self.assertEqual(cm.exception.status, 401)

    def test_batch_fans_out_in_order(self):
        results = self.mesh.batch([{"messages": [{"role": "user", "content": str(i)}]} for i in range(3)])
        self.assertEqual(len(results), 3)
        for r in results:
            self.assertEqual(r["status"], 200)

    def test_usage_ledger_capacity_shapes(self):
        self.assertIn("usage", self.mesh.usage())
        self.assertIn("contributed", self.mesh.ledger())
        self.assertIn("nodes", self.mesh.capacity())

    def test_share_get_and_set_roundtrip(self):
        applied = self.mesh.set_share(cpu_percent=10)
        self.assertEqual(applied["limits"]["cpu_percent"], 10)
        self.assertEqual(self.mesh.share()["limits"]["cpu_percent"], 10)

    def test_store_empty_on_fresh_node(self):
        self.assertEqual(self.mesh.store(), [])

    def test_extensions_empty_on_fresh_node(self):
        self.assertEqual(self.mesh.extensions(), [])

    def test_replication_reports_disabled_without_pki(self):
        rep = self.mesh.replication()
        # No PKI/mesh in this harness — the node reports it cleanly, not a 500.
        self.assertEqual(rep.get("enabled"), False)


if __name__ == "__main__":
    unittest.main()

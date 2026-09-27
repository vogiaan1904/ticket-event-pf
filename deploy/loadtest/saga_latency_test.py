"""The saga latency decomposition. Run: python3 deploy/loadtest/saga_latency_test.py"""
import os
import sys
import tempfile
import unittest

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import saga_latency  # noqa: E402


def event(event_id, secs, kind, **attrs):
    e = {"eventId": str(event_id), "eventTime": f"2026-09-27T06:43:{10 + secs:09.6f}Z",
         "eventType": f"EVENT_TYPE_{kind}"}
    e.update(attrs)
    return e


# One saga: its first workflow task waits 1.0s, and ReserveInventory waits 0.2s
# for a worker, then runs 0.3s on its second attempt.
HISTORY = [
    event(1, 0.0, "WORKFLOW_EXECUTION_STARTED"),
    event(2, 0.0, "WORKFLOW_TASK_SCHEDULED"),
    event(3, 1.0, "WORKFLOW_TASK_STARTED"),
    event(5, 1.1, "ACTIVITY_TASK_SCHEDULED",
          activityTaskScheduledEventAttributes={"activityType": {"name": "ReserveInventory"}}),
    event(6, 1.3, "ACTIVITY_TASK_STARTED",
          activityTaskStartedEventAttributes={"scheduledEventId": "5", "attempt": 2}),
    event(7, 1.6, "ACTIVITY_TASK_COMPLETED",
          activityTaskCompletedEventAttributes={"scheduledEventId": "5"}),
    event(8, 1.7, "WORKFLOW_EXECUTION_COMPLETED"),
]


class ParseTest(unittest.TestCase):
    def test_splits_each_step_into_queued_and_ran(self):
        r = saga_latency.parse("CreateOrder:TB-1", HISTORY)
        self.assertEqual(r["code"], "TB-1")
        self.assertAlmostEqual(r["total"], 1.7)
        self.assertAlmostEqual(r["wft_queued"][0], 1.0)
        self.assertAlmostEqual(r["queued"]["ReserveInventory"], 0.2)
        self.assertAlmostEqual(r["ran"]["ReserveInventory"], 0.3)
        self.assertEqual(r["retries"], 1)

    def test_reads_checkout_lines_from_a_k6_log(self):
        with tempfile.NamedTemporaryFile("w", suffix=".log", delete=False) as f:
            f.write('time="2026-09-27T06:43:12Z" level=info msg="CHECKOUT TB-1 2150.4 201" source=console\n'
                    'time="2026-09-27T06:43:13Z" level=info msg="CHECKOUT - 12.0 409" source=console\n'
                    'time="2026-09-27T06:43:14Z" level=info msg="other" source=console\n')
        self.addCleanup(os.unlink, f.name)
        got = saga_latency.load_client(f.name)
        self.assertEqual([(code, status) for code, _, status in got], [("TB-1", 201), ("-", 409)])
        self.assertAlmostEqual(got[0][1], 2.1504)
        self.assertAlmostEqual(got[1][1], 0.012)

    def test_default_burst_spans_two_admission_ticks(self):
        # Run A's shape: two ticks 0-2.7s, then the next saga 2.8s later.
        runs = [saga_latency.parse(f"CreateOrder:TB-{i}", [event(1, secs, "WORKFLOW_EXECUTION_STARTED")])
                for i, secs in enumerate([0.0, 1.3, 2.7, 5.5])]
        burst, steady = saga_latency.split_burst(runs)
        self.assertEqual([r["code"] for r in burst], ["TB-0", "TB-1", "TB-2"])
        self.assertEqual([r["code"] for r in steady], ["TB-3"])

    def test_percentile_is_nearest_rank(self):
        xs = [float(x) for x in range(1, 101)]
        self.assertEqual(saga_latency.pct(xs, 50), 51.0)
        self.assertEqual(saga_latency.pct(xs, 99), 99.0)


if __name__ == "__main__":
    unittest.main()

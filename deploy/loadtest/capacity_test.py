"""The capacity run summary. Run: python3 deploy/loadtest/capacity_test.py"""
import json
import os
import sys
import tempfile
import unittest

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import capacity  # noqa: E402

# Nothing binds: every signal well under its threshold.
CALM = {"vcpu": 16, "busy_cores": 4.0, "iops": 500.0, "busiest_node_js": ("app-gateway-a", 0.3),
        "pg_shares": {"CPU": 1.0}, "lock_db": "-", "first_task_p50": 0.05, "unexpected_share": 0.0}


def signals(**changes):
    s = dict(CALM)
    s.update(changes)
    return s


class PgSummary(unittest.TestCase):
    LINES = [
        "S|99|50|500",                                  # before the run
        "W|99|Lock|transactionid|ticketbottle_inventory|9",
        "S|100|120|500",
        "W|100|CPU|-|temporal|2",
        "W|100|IO|WALSync|temporal|1",
        "S|102|80|500",
        "W|102|LWLock|WALWrite|temporal|1",
        "W|102|Lock|transactionid|ticketbottle_inventory|4",
        "psql: error: connection to server failed",      # a sample that failed
        "S|200|900|500",                                 # after the run
    ]

    def test_counts_only_samples_inside_the_run(self):
        peak, limit, shares, lock_db = capacity.pg_summary(self.LINES, 100, 200)
        self.assertEqual((peak, limit), (120, 500))
        self.assertEqual(shares, {"CPU": 0.25, "IO": 0.25, "Lock": 0.5})
        self.assertEqual(lock_db, "ticketbottle_inventory")

    def test_a_wal_lwlock_waits_on_the_disk(self):
        _, _, shares, _ = capacity.pg_summary(["W|5|LWLock|WALWrite|temporal|3"], 0, 10)
        self.assertEqual(shares, {"IO": 1.0})

    def test_no_active_session_is_no_share(self):
        self.assertEqual(capacity.pg_summary(["S|5|10|100"], 0, 10), (10, 100, {}, "-"))


class Verdict(unittest.TestCase):
    def test_nothing_binds(self):
        self.assertEqual(capacity.verdict(CALM), [])

    def test_each_limit_on_its_own(self):
        cases = [
            (signals(busiest_node_js=("app-gateway-a", 0.9)), ["event loop: app-gateway-a"]),
            (signals(busy_cores=13.6), ["cpu"]),
            (signals(iops=2400.0), ["disk"]),
            (signals(pg_shares={"IO": 0.6, "CPU": 0.4}), ["disk"]),
            (signals(pg_shares={"Lock": 0.5, "CPU": 0.5}, lock_db="ticketbottle_inventory"),
             ["locks: ticketbottle_inventory"]),
            (signals(first_task_p50=0.5), ["temporal dispatch"]),
            (signals(unexpected_share=0.02), ["errors"]),
        ]
        for s, want in cases:
            self.assertEqual(capacity.verdict(s), want)

    def test_just_under_every_threshold_binds_nothing(self):
        s = signals(busiest_node_js=("app-gateway-a", 0.84), busy_cores=13.5, iops=2399.0,
                    pg_shares={"IO": 0.49, "Lock": 0.49}, first_task_p50=0.49, unexpected_share=0.01)
        self.assertEqual(capacity.verdict(s), [])

    def test_a_cause_comes_before_its_symptoms(self):
        s = signals(busy_cores=15.0, first_task_p50=2.0, unexpected_share=0.05)
        self.assertEqual(capacity.verdict(s), ["cpu", "temporal dispatch", "errors"])


class Void(unittest.TestCase):
    META = {"restarts_before": 3, "restarts_after": 3}

    def test_a_clean_run_counts(self):
        self.assertEqual(capacity.void(self.META, 10, 1000), [])

    def test_a_restart_voids_the_run(self):
        self.assertEqual(capacity.void({"restarts_before": 3, "restarts_after": 5}, 0, 1000),
                         ["2 container restarts"])

    def test_a_throttled_or_unread_load_generator_voids_the_run(self):
        self.assertEqual(capacity.void(self.META, 60, 1000), ["k6 throttled 6%"])
        self.assertEqual(capacity.void(self.META, 0, 0), ["k6 throttling unread"])


def prom(path, label, values):
    result = [{"metric": {label: k} if label else {}, "value": [0, str(v)]} for k, v in values.items()]
    with open(path, "w") as f:
        json.dump({"status": "success", "data": {"resultType": "vector", "result": result}}, f)


class Summarize(unittest.TestCase):
    def test_one_run_reads_back_as_its_numbers(self):
        with tempfile.TemporaryDirectory() as d:
            # A 100s run on 16 vCPUs: 250 purchases, 12 cores busy, 2500 IOPS.
            with open(f"{d}/meta.json", "w") as f:
                json.dump({"label": "v80-1", "vcpu": 16, "vus": 80, "from": "2026-10-09T10:00:00Z",
                           "to": "2026-10-09T10:01:40Z", "restarts_before": 0, "restarts_after": 0}, f)
            with open(f"{d}/k6.log", "w") as f:
                f.write('time="x" level=info msg="CHECKOUT TB-1 900.0 201" source=console\n'
                        'time="x" level=info msg="CHECKOUT TB-2 1100.0 201" source=console\n'
                        '{"metrics": {"tb_orders_completed": {"count": 250, "rate": 2.5}}}\n')
            prom(f"{d}/pods-from.json", "pod", {"temporal-abc-def": 10, "postgres-0": 5, "app-gateway-x-y": 0})
            prom(f"{d}/pods-to.json", "pod", {"temporal-abc-def": 310, "postgres-0": 205, "app-gateway-x-y": 60,
                                              "k6-load-z": 50})
            prom(f"{d}/node-from.json", "mode", {"user": 0, "system": 0, "idle": 0, "iowait": 0, "steal": 0})
            prom(f"{d}/node-to.json", "mode", {"user": 1000, "system": 200, "idle": 400, "iowait": 50, "steal": 30})
            prom(f"{d}/disk-from.json", "", {"": 1000})
            prom(f"{d}/disk-to.json", "", {"": 251000})
            prom(f"{d}/k6-throttled.json", "pod", {"k6-load-z": 1})
            prom(f"{d}/k6-periods.json", "pod", {"k6-load-z": 1000})
            with open(f"{d}/pg.txt", "w") as f:
                f.write("S|1791540000|130|500\nW|1791540000|CPU|-|temporal|3\nW|1791540000|IO|WALSync|temporal|1\n")
            open(f"{d}/histories.txt", "w").close()

            line, busiest = capacity.summarize(d)

        self.assertTrue(line.startswith("v80-1 16 vCPU 80 buyers | 2.50 purchases/s,"), line)
        self.assertIn("busy 12.0/16 iowait 0.5 | 2500 IOPS", line)
        self.assertIn("pg 130/500 conns, CPU 75% IO 25% Lock 0%", line)
        self.assertIn("first task p50/max n/a", line)
        self.assertIn("core-s/purchase temporal 1.200 postgres 0.800", line)
        self.assertTrue(line.endswith("0 unexpected -> disk"), line)
        self.assertEqual(busiest, "  busiest: temporal-abc-def 3.00, postgres-0 2.00, app-gateway-x-y 0.60")


if __name__ == "__main__":
    unittest.main()

"""The ConfirmOrder overlap summary. Run: python3 deploy/loadtest/confirm_overlap_test.py"""
import json
import os
import sys
import unittest

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import confirm_overlap  # noqa: E402


def line(start, close=None):
    r = {"type": {"name": "ConfirmOrder"}, "startTime": start}
    if close:
        r["closeTime"] = close
    return json.dumps(r)


class Peak(unittest.TestCase):
    def test_two_open_at_once(self):
        self.assertEqual(confirm_overlap.peak([(0, 1), (0.5, 1.5)]), 2)

    def test_one_closing_as_the_next_starts_is_not_an_overlap(self):
        self.assertEqual(confirm_overlap.peak([(0, 1), (1, 2)]), 1)

    def test_none(self):
        self.assertEqual(confirm_overlap.peak([]), 0)


class Intervals(unittest.TestCase):
    def test_closed_workflows_in_start_order_and_running_ones_skipped(self):
        spans = confirm_overlap.intervals([
            line("2026-10-11T08:27:29.841762Z", "2026-10-11T08:27:29.996747Z"),
            line("2026-10-11T08:27:29.500000123Z", "2026-10-11T08:27:29.600000Z"),
            line("2026-10-11T08:27:30Z"),
            "",
        ])
        self.assertEqual(len(spans), 2)
        self.assertAlmostEqual(spans[0][1] - spans[0][0], 0.099999877, places=6)
        self.assertAlmostEqual(spans[1][1] - spans[1][0], 0.154985, places=6)


class Summary(unittest.TestCase):
    def test_one_line(self):
        self.assertEqual(confirm_overlap.summary([(0, 0.1), (0.05, 0.2), (1.0, 1.1)]),
                         "3 confirms over 1.1s, at most 2 at once, mean 117ms, 2.73/s")

    def test_none(self):
        self.assertEqual(confirm_overlap.summary([]), "0 confirms")


if __name__ == "__main__":
    unittest.main()

"""The decision index's contract. Run: python3 -m unittest docs/decisions/index_test.py"""
import os
import sys
import tempfile
import unittest

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import index  # noqa: E402

README = f"# Decision records\n\n{index.START}\n{index.END}\n"


def record(num, arc="arc-a", status="accepted", row="| Q | C | I | K |", title=None, date="2026-09-23"):
    lines = [f"# {num} — {title or 'A decision'}", ""]
    if date:
        lines.append(f"**Date:** {date}")
    if status:
        lines.append(f"**Status:** {status}")
    if arc:
        lines.append(f"**Arc:** {arc} — [plan](../plans/x.md)")
    lines.append("")
    if row:
        lines += ["| Question | Chose | Instead of | Cost |", "|---|---|---|---|", row]
    lines += ["", "## Context", "x"]
    return "\n".join(lines) + "\n"


class IndexTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.dir = self.tmp.name
        self.write("README.md", README)

    def tearDown(self):
        self.tmp.cleanup()

    def write(self, name, text):
        with open(os.path.join(self.dir, name), "w") as f:
            f.write(text)

    def readme(self):
        with open(os.path.join(self.dir, "README.md")) as f:
            return f.read()

    def test_a_record_is_listed_under_its_arc_linked_and_with_its_status(self):
        self.write("0003-poll.md", record("0003", arc="waitroom", row="| How? | Poll | Push | Latency |"))
        self.assertEqual(index.main([self.dir]), 0)
        out = self.readme()
        self.assertIn("### waitroom", out)
        self.assertIn("| [0003](0003-poll.md) | How? | Poll | Push | Latency | accepted |", out)

    def test_arcs_are_ordered_by_their_first_record(self):
        self.write("0002-b.md", record("0002", arc="zeta"))
        self.write("0003-c.md", record("0003", arc="alpha"))
        self.write("0004-d.md", record("0004", arc="zeta"))
        index.main([self.dir])
        out = self.readme()
        self.assertLess(out.index("### zeta"), out.index("### alpha"))
        self.assertEqual(out.count("### zeta"), 1)

    def test_a_superseded_record_stays_listed_and_names_its_successor(self):
        self.write("0001-old.md", record("0001", status="superseded by [0002](0002-new.md) on 2026-09-22"))
        self.write("0002-new.md", record("0002"))
        index.main([self.dir])
        self.assertIn("| [0001](0001-old.md) | Q | C | I | K | superseded by 0002 |", self.readme())

    def test_an_incomplete_record_fails_and_names_the_file(self):
        for missing in ("row", "arc", "date", "status"):
            with self.subTest(missing=missing):
                self.write("0005-bad.md", record("0005", **{missing: None}))
                with self.assertRaises(index.RecordError) as ctx:
                    index.load_records(self.dir)
                self.assertIn("0005-bad.md", str(ctx.exception))

    def test_an_unknown_status_fails(self):
        self.write("0005-bad.md", record("0005", status="maybe"))
        with self.assertRaises(index.RecordError):
            index.load_records(self.dir)

    def test_a_title_numbered_unlike_its_file_fails(self):
        self.write("0006-x.md", record("0007"))
        with self.assertRaises(index.RecordError) as ctx:
            index.load_records(self.dir)
        self.assertIn("0006-x.md", str(ctx.exception))

    def test_check_passes_when_fresh_and_fails_when_stale(self):
        self.write("0003-a.md", record("0003"))
        index.main([self.dir])
        self.assertEqual(index.main([self.dir, "--check"]), 0)
        self.write("0004-b.md", record("0004"))
        self.assertEqual(index.main([self.dir, "--check"]), 1)


if __name__ == "__main__":
    unittest.main()

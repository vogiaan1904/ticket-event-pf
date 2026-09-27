"""The system map's check. Run: python3 .claude/skills/system-map/scripts/check_map_test.py"""
import contextlib
import io
import os
import subprocess
import sys
import tempfile
import unittest

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import check_map  # noqa: E402

SKILL = ".claude/skills/system-map"
GUIDE = f"{SKILL}/references/order.md"
CLEAN_GUIDE = (
    "Read `services/order-svc/CLAUDE.md`, then "
    "`services/order-svc/internal/workflows/create_order.go`.\n"
)


class CheckMapTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.root = self.tmp.name
        subprocess.run(["git", "init", "-q"], cwd=self.root, check=True)
        self.write("CLAUDE.md", "root\n")
        self.write("services/order-svc/CLAUDE.md", "order\n")
        self.write("services/order-svc/internal/workflows/create_order.go", "package workflows\n")
        self.write(GUIDE, CLEAN_GUIDE)
        self.skill_md("Start at `CLAUDE.md`. Services: [order](references/order.md).\n")

    def tearDown(self):
        self.tmp.cleanup()

    def write(self, rel, text):
        path = os.path.join(self.root, rel)
        os.makedirs(os.path.dirname(path), exist_ok=True)
        with open(path, "w") as f:
            f.write(text)

    def skill_md(self, body):
        self.write(f"{SKILL}/SKILL.md", "---\nname: system-map\n---\n\n" + body)

    def track(self):
        subprocess.run(["git", "add", "-A"], cwd=self.root, check=True)

    def problems(self, track=True):
        if track:
            self.track()
        return check_map.problems(self.root, os.path.join(self.root, SKILL))

    def test_clean_map_passes(self):
        self.assertEqual(self.problems(), [])

    def test_missing_path_is_reported(self):
        self.write(GUIDE, CLEAN_GUIDE + "Also `services/order-svc/internal/workflows/gone.go`.\n")
        found = self.problems()
        self.assertEqual(len(found), 1, found)
        self.assertIn("references/order.md:2", found[0])
        self.assertIn("services/order-svc/internal/workflows/gone.go", found[0])

    def test_uncited_doc_is_reported(self):
        self.write("services/order-svc/docs/NEW.md", "a new design\n")
        found = self.problems()
        self.assertEqual(len(found), 1, found)
        self.assertIn("services/order-svc/docs/NEW.md", found[0])

    def test_untracked_doc_is_not_required(self):
        self.track()
        self.write("docs/labs/notes.md", "local only\n")
        self.assertEqual(self.problems(track=False), [])

    def test_ignored_doc_is_not_required(self):
        self.write(".gitignore", "tools/\n")
        self.write("tools/seed-loader/README.md", "ignored\n")
        self.assertEqual(self.problems(), [])

    def test_suffixes_are_stripped(self):
        self.write(GUIDE, (
            "`services/order-svc/CLAUDE.md#role` and "
            "`services/order-svc/internal/workflows/create_order.go:12-40`.\n"
        ))
        self.assertEqual(self.problems(), [])

    def test_placeholders_are_not_paths(self):
        self.skill_md(
            "Start at `CLAUDE.md`. [order](references/order.md). Each service has "
            "`services/<svc>/CLAUDE.md`; Go ones are `services/{order,inventory}-svc/`; "
            "records are `docs/decisions/NNNN-*.md`.\n"
        )
        self.assertEqual(self.problems(), [])

    def test_non_path_spans_are_ignored(self):
        self.skill_md(
            "Start at `CLAUDE.md`. [order](references/order.md). Topics like "
            "`queue.ready`; `make -C deploy k3s-gate2`; image `ticketbottle/payment-events`; "
            "the `dtos/req` split.\n"
        )
        self.assertEqual(self.problems(), [])

    def test_relative_code_path_is_reported(self):
        self.write(GUIDE, CLEAN_GUIDE + "See `internal/workflows/create_order.go`.\n")
        found = self.problems()
        self.assertEqual(len(found), 1, found)
        self.assertIn("repo-relative", found[0])

    def test_broken_link_is_reported(self):
        self.skill_md("Start at `CLAUDE.md`. [order](references/order.md), [x](references/gone.md).\n")
        found = self.problems()
        self.assertEqual(len(found), 1, found)
        self.assertIn("references/gone.md", found[0])

    def test_excluded_kinds_are_not_required(self):
        for rel in (
            "docs/plans/2026-01-01-work.md",
            "docs/decisions/0001-a-call.md",
            "services/order-svc/node_modules/pkg/README.md",
            "services/order-svc/vendor/lib/README.md",
            ".claude/hooks/stance.md",
            ".claude/skills/other/references/deep.md",
        ):
            self.write(rel, "x\n")
        self.write(".claude/skills/other/SKILL.md", "x\n")
        self.skill_md(
            "Start at `CLAUDE.md`. [order](references/order.md). "
            "Deploys: `.claude/skills/other/SKILL.md`.\n"
        )
        self.assertEqual(self.problems(), [])

    def test_another_skill_must_be_cited(self):
        self.write(".claude/skills/other/SKILL.md", "x\n")
        found = self.problems()
        self.assertEqual(len(found), 1, found)
        self.assertIn(".claude/skills/other/SKILL.md", found[0])

    def test_cli_exits_nonzero_on_a_problem(self):
        self.write("docs/design/uncited.md", "x\n")
        self.track()
        with contextlib.redirect_stdout(io.StringIO()) as out:
            self.assertEqual(check_map.main(["--root", self.root]), 1)
        self.assertIn("docs/design/uncited.md", out.getvalue())
        subprocess.run(["git", "rm", "-qf", "docs/design/uncited.md"], cwd=self.root, check=True)
        with contextlib.redirect_stdout(io.StringIO()):
            self.assertEqual(check_map.main(["--root", self.root]), 0)


if __name__ == "__main__":
    unittest.main()

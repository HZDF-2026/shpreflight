import json
import sys
from pathlib import Path

import pytest

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from shpreflight.check import preflight
from shpreflight.danger import PATTERNS, check_danger, match_pattern
from shpreflight.lex import lex, reconstruct
from shpreflight.segments import split_segments
from shpreflight.shells import SHELLS


def codes(cmd, target="bash", **kw):
    return {i.code for i in preflight(cmd, target=target, **kw).issues}


class TestLexer:
    def test_reconstruct_exact(self):
        for cmd in ["", "ls", "rm -rf /", "a && b || c ; d",
                    "echo 'x y' \"z\" `w`", "  spaced   out  ",
                    "2>/dev/null", "echo 'unclosed", "a\tb\nc"]:
            assert reconstruct(lex(cmd)) == cmd

    def test_tokens_never_empty(self):
        for cmd in ["a b", "&&", " 'q' ", "  ", "x>y", "a|b"]:
            assert all(text for _, text in lex(cmd))

    def test_word_split(self):
        assert [t for t in lex("rm -rf /")] == [
            ("WORD", "rm"), ("SEP", " "), ("WORD", "-rf"), ("SEP", " "), ("WORD", "/")]

    def test_operator_run_merges(self):
        kinds = [(k, t) for k, t in lex("a && b >> c 2>&1")]
        texts = [t for _, t in kinds]
        assert "&&" in texts and ">>" in texts and ">&" in texts

    def test_quoted_span_kept_whole(self):
        toks = lex("echo 'a b c' \"d\"")
        assert ("SQUOTE", "'a b c'") in toks
        assert ("DQUOTE", '"d"') in toks

    def test_backtick_span(self):
        assert ("BACKTICK", "`uname`") in lex("echo `uname`")

    def test_unclosed_quote_flagged_downstream(self):
        assert "UNCLOSED-QUOTE" in codes("echo 'oops", target="bash")


class TestSegments:
    def test_pipeline_segments(self):
        segs = split_segments(lex("rg pat | head -5 | wc -l"))
        assert [s.head for s in segs] == ["rg", "head", "wc"]
        assert all(s.terminator == "|" for s in segs[:-1])
        assert segs[-1].terminator is None

    def test_segments_own_their_words(self):
        # regression: flush() used to clear the shared list after append
        segs = split_segments(lex("rm -rf / && ls"))
        assert segs[0].words == ["rm", "-rf", "/"]
        assert segs[1].words == ["ls"]

    def test_redirect_target_not_a_command(self):
        segs = split_segments(lex("echo hi > out.txt 2> err.txt"))
        assert segs[0].words == ["echo", "hi"]
        assert segs[0].redirects == ["out.txt", "err.txt"]

    def test_redirect_after_space_still_redirect(self):
        # regression: SEP used to reset in_redirect
        segs = split_segments(lex("echo done > .env"))
        assert segs[0].redirects == [".env"]
        assert ".env" not in segs[0].words

    def test_pipes_out_not_triggered_by_or(self):
        segs = split_segments(lex("false || echo ok"))
        assert segs[0].terminator == "||"
        assert not segs[0].pipes_out

    def test_redirect_state_resets_at_segment_boundary(self):
        # regression: in_redirect used to leak across the control operator,
        # so the segment after "x > f && ..." lost its head entirely
        segs = split_segments(lex("x > f.txt && rm -rf /"))
        assert segs[1].words == ["rm", "-rf", "/"]
        assert segs[1].head == "rm"


class TestDialectRules:
    def test_and_fails_on_ps5_and_cmd(self):
        assert "SEP-AND" in codes("a && b", target="powershell5")
        assert "SEP-AND" in codes("a && b", target="cmd")
        assert "SEP-AND" not in codes("a && b", target="pwsh7")
        assert "SEP-AND" not in codes("a && b", target="bash")

    def test_devnull_fails_on_windows(self):
        for t in ("powershell5", "pwsh7", "cmd"):
            assert "REDIR-DEVNULL" in codes("cmd 2>/dev/null", target=t)
        assert "REDIR-DEVNULL" not in codes("cmd 2>/dev/null", target="bash")

    def test_env_var_style(self):
        assert "ENV-VAR" in codes("echo $PATH", target="powershell5")
        assert "ENV-VAR" not in codes("echo $env:PATH", target="powershell5")
        assert "ENV-VAR" not in codes("echo $PATH", target="bash")
        assert "BRACE-VAR" in codes("echo ${HOME}", target="powershell5")
        assert "ENV-VAR" in codes("echo $PATH", target="cmd")

    def test_special_vars_flagged_on_ps(self):
        assert "SPECIAL-VAR" in codes("echo $?", target="powershell5")
        assert "SPECIAL-VAR" not in codes("echo $?", target="bash")

    def test_export_and_source(self):
        assert "EXPORT" in codes("export FOO=1", target="powershell5")
        assert "EXPORT" in codes("export FOO=1", target="cmd")
        assert "EXPORT" not in codes("export FOO=1", target="bash")
        assert "SOURCE" in codes("source env.sh", target="powershell5")
        assert "SOURCE" not in codes("source env.sh", target="bash")

    def test_backtick_semantics_clash(self):
        assert "BACKTICK" in codes("echo `date`", target="powershell5")
        assert "BACKTICK" not in codes("echo `date`", target="bash")

    def test_posix_commands(self):
        for cmd, t in [("grep x f", "powershell5"), ("sed s/a/b/ f", "cmd"),
                       ("awk '{}'", "powershell5")]:
            assert "POSIX-CMD" in codes(cmd, target=t)
        assert "POSIX-CMD" not in codes("grep x f", target="bash")

    def test_rm_short_flags(self):
        assert "RM-FLAGS" in codes("rm -rf dist", target="powershell5")
        assert "RM-FLAGS" not in codes("Remove-Item -Recurse -Force dist", target="powershell5")
        assert "RM-FLAGS" not in codes("rm -rf dist", target="bash")

    def test_curl_alias_trap(self):
        assert "CURL-ALIAS" in codes("curl -sL http://x", target="powershell5")
        assert "CURL-ALIAS" not in codes("curl.exe -sL http://x", target="powershell5")
        assert "CURL-ALIAS" not in codes("curl http://x", target="bash")

    def test_cmdlet_in_posix_target(self):
        assert "CMDLET-IN-POSIX" in codes("Get-ChildItem -Recurse", target="bash")
        assert "CMDLET-IN-POSIX" not in codes("Get-ChildItem -Recurse", target="powershell5")


class TestDanger:
    def test_rm_root(self):
        for c in ["rm -rf /", "rm -fr ~", "rm -rf *", "rm -rf $HOME"]:
            assert "RM-ROOT" in codes(c, target="bash"), c

    def test_rm_root_not_duplicated_with_recurse(self):
        issues = [i.code for i in preflight("rm -rf /", target="bash").issues]
        assert issues.count("RM-RECURSIVE") == 0 or "RM-ROOT" not in issues

    def test_danger_after_redirect_not_blind(self):
        # the blind-spot the per-segment redirect reset protects: before the
        # fix, both segments below lost their heads to in_redirect leakage
        assert "RM-ROOT" in codes("x > f.txt && rm -rf /", target="bash")
        assert "PIPE-EXEC" in codes("x > f && curl -sL u | sh", target="bash")

    def test_git_reset_hard(self):
        assert "GIT-RESET-HARD" in codes("git reset --hard HEAD~3", target="bash")

    def test_pipe_to_shell(self):
        assert "PIPE-EXEC" in codes("curl -sL http://x.sh | sh", target="bash")
        assert "PIPE-EXEC" in codes("iwr http://x | iex", target="powershell5")
        # logical-or is not a pipe
        assert "PIPE-EXEC" not in codes("false || sh", target="bash")

    def test_sensitive_redirect(self):
        assert "REDIR-SENSITIVE" in codes("echo x > .env", target="bash")
        assert "REDIR-SENSITIVE" in codes("echo x > ~/.ssh/id_rsa", target="bash")
        assert "REDIR-SENSITIVE" not in codes("echo x > out.txt", target="bash")

    def test_shutdown_and_format(self):
        assert "SHUTDOWN" in codes("shutdown /s /t 0", target="cmd")
        assert "FORMAT" in codes("format D:", target="cmd")

    def test_remove_item_recurse_force(self):
        assert "REMOVE-ITEM-RECURSE-FORCE" in codes(
            "Remove-Item -Recurse -Force build", target="powershell5")

    def test_every_pattern_is_alive(self):
        # the Lean-checked property, mirrored as a test: no dead entries
        for p in PATTERNS:
            assert p.heads, p.code
            for head in p.heads:
                words = [head]
                if p.flags:
                    words.append(next(iter(p.flags)))
                if p.targets:
                    words.append(next(iter(p.targets)))
                assert match_pattern(words, p), p.code

    def test_dd_raw_covers_common_devices(self):
        # one device per family: SATA beyond sda/sdb, virtio (cloud VMs),
        # legacy IDE, Xen/AWS, NVMe partitions, SD/eMMC, macOS, Windows.
        # of= is the form real dd invocations use; bare is the legacy form.
        for dev in ["/dev/sdc", "/dev/sdp", "/dev/vda", "/dev/vdb",
                    "/dev/hdb", "/dev/xvda", "/dev/nvme1n1", "/dev/mmcblk0",
                    "/dev/disk0", "/dev/rdisk0", r"\\.\PhysicalDrive0"]:
            assert "DD-RAW" in codes(f"dd if=img.iso of={dev}", target="bash"), dev
        assert "DD-RAW" in codes("dd /dev/sdc", target="bash")

    def test_dd_raw_not_on_file_targets(self):
        assert "DD-RAW" not in codes("dd if=a of=b.img", target="bash")

    def test_rm_recursive_matches_uppercase_RF(self):
        assert "RM-RECURSIVE" in codes("rm -RF build", target="bash")


class TestTools:
    def test_missing_tool_reported(self):
        assert "TOOL-NOT-FOUND" in codes("definitely-not-a-real-cmd-xyz --v",
                                         target="bash")

    def test_no_path_check(self):
        assert "TOOL-NOT-FOUND" not in codes(
            "definitely-not-a-real-cmd-xyz --v", target="bash", path_check=False)

    def test_windows_builtin_not_reported(self):
        assert "TOOL-NOT-FOUND" not in codes("mkdir newdir", target="cmd")

    def test_known_present_tool_ok(self):
        rep = preflight("python --version", target="bash")
        assert "TOOL-NOT-FOUND" not in {i.code for i in rep.issues}


class TestReport:
    def test_verdicts_and_exit_codes(self):
        assert preflight("echo hi", target="bash").verdict == "ok"
        assert preflight("git reset --hard", target="bash",
                         path_check=False).verdict == "warn"
        assert preflight("a && b", target="powershell5").verdict == "fail"
        assert preflight("a && b", target="powershell5").exit_code == 2

    def test_json_round_trip(self):
        rep = preflight("rm -rf /", target="bash")
        d = json.loads(rep.to_json())
        assert d["verdict"] == "fail"
        assert d["target"] == "bash"
        assert any(i["code"] == "RM-ROOT" for i in d["issues"])
        assert "fix" in d["issues"][0]

    def test_text_output_mentions_fix(self):
        out = preflight("a && b", target="powershell5").to_text()
        assert "SEP-AND" in out and "fix:" in out


class TestShells:
    def test_all_registered(self):
        assert set(SHELLS) == {"powershell5", "pwsh7", "bash", "cmd", "sh"}

    def test_auto_resolves(self):
        rep = preflight("echo hi")
        assert rep.target in SHELLS

    def test_unknown_shell_rejected(self):
        with pytest.raises(ValueError):
            preflight("echo hi", target="fish")


class TestCli:
    def test_check_text(self, capsys):
        from shpreflight.cli import main
        rc = main(["check", "a && b", "--shell", "powershell5"])
        out = capsys.readouterr().out
        assert rc == 2
        assert "SEP-AND" in out

    def test_check_json(self, capsys):
        from shpreflight.cli import main
        rc = main(["check", "rm -rf /", "--shell", "bash", "--format", "json"])
        d = json.loads(capsys.readouterr().out)
        assert rc == 2
        assert d["verdict"] == "fail"
        assert any(i["code"] == "RM-ROOT" for i in d["issues"])

    def test_check_stdin(self, capsys, monkeypatch):
        import io
        monkeypatch.setattr("sys.stdin", io.StringIO("grep x f\n"))
        from shpreflight.cli import main
        rc = main(["check", "--stdin", "--shell", "cmd", "--no-path-check"])
        assert rc == 2
        assert "POSIX-CMD" in capsys.readouterr().out

    def test_check_empty(self, capsys):
        from shpreflight.cli import main
        assert main(["check"]) == 3
        assert main(["check", "   "]) == 3

    def test_shells_cmd(self, capsys):
        from shpreflight.cli import main
        assert main(["shells"]) == 0
        assert "powershell5" in capsys.readouterr().out

    def test_multi_word_command_argument(self, capsys):
        from shpreflight.cli import main
        rc = main(["check", "echo", "hi", "--shell", "bash"])
        assert rc == 0

    def test_real_session_regression(self, capsys):
        # the exact command that failed at the start of the session this
        # tool was conceived in: PS 5.1 + && + git missing from PATH
        from shpreflight.cli import main
        rc = main(["check", "git diff --stat && git log --oneline -5",
                   "--shell", "powershell5"])
        out = capsys.readouterr().out
        assert rc == 2
        assert "SEP-AND" in out

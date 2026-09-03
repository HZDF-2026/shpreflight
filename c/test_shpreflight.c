/*
 * test_shpreflight.c — embedded unit tests for the C port.
 *
 * Build & run (single translation unit, includes the implementation):
 *   cc -O2 -std=c99 -Wall -Wextra -DSHPREFLIGHT_NO_MAIN -o test_shpreflight test_shpreflight.c
 *   ./test_shpreflight
 *
 * Exit code: 0 = all checks passed, 1 = failures.
 */

#define SHPREFLIGHT_NO_MAIN 1
#include "shpreflight.c"

#if defined(_WIN32)
#  include <direct.h>
#  include <stdlib.h>
#  define MKDIR(p) _mkdir(p)
#  define RMDIR(p) _rmdir(p)
#else
#  include <sys/stat.h>
#  include <sys/types.h>
#  include <unistd.h>
#  define MKDIR(p) mkdir(p, 0777)
#  define RMDIR(p) rmdir(p)
#endif

#include <stdio.h>
#include <string.h>

static int checks = 0;
static int failures = 0;

#define CHECK(cond, msg)                                          \
    do {                                                          \
        checks++;                                                 \
        if (!(cond)) {                                            \
            failures++;                                           \
            printf("FAIL %s:%d: %s\n", __FILE__, __LINE__, (msg)); \
        }                                                         \
    } while (0)

/* ---------- helpers ---------- */

static int has_code(const Report *r, const char *code)
{
    for (size_t i = 0; i < r->nissues; i++)
        if (strcmp(r->issues[i].code, code) == 0)
            return 1;
    return 0;
}

static const Issue *find_code(const Report *r, const char *code)
{
    for (size_t i = 0; i < r->nissues; i++)
        if (strcmp(r->issues[i].code, code) == 0)
            return &r->issues[i];
    return NULL;
}

/* run preflight with PATH resolution disabled (deterministic) */
static int check_code(const char *cmd, const char *target, const char *code)
{
    Report r;
    const char *err = NULL;
    if (preflight(cmd, target, 0, &r, &err) != 0)
        return 0;
    return has_code(&r, code);
}

static Segments segs_of(const char *cmd)
{
    Tokens toks;
    Segments segs;
    memset(&toks, 0, sizeof toks);
    memset(&segs, 0, sizeof segs);
    lex(cmd, strlen(cmd), &toks);
    split_segments(&toks, &segs);
    return segs;
}

/* ---------- lexer ---------- */

static void test_lexer_reconstruct(void)
{
    const char *cmds[] = {
        "", "ls", "rm -rf /", "a && b || c ; d",
        "echo 'x y' \"z\" `w`", "  spaced   out  ",
        "2>/dev/null", "echo 'unclosed", "a\tb\nc",
        "x>>f", "a|b", "'q'", "2>&1"
    };
    for (size_t c = 0; c < sizeof cmds / sizeof cmds[0]; c++) {
        Tokens t;
        memset(&t, 0, sizeof t);
        lex(cmds[c], strlen(cmds[c]), &t);
        CHECK(reconstruct_eq(&t, cmds[c], strlen(cmds[c])), "reconstruct(lex(cmd)) != cmd");
        for (size_t i = 0; i < t.n; i++)
            CHECK(t.v[i].text.n > 0, "token text must never be empty");
    }
}

static void test_word_split(void)
{
    Tokens t;
    memset(&t, 0, sizeof t);
    lex("rm -rf /", 8, &t);
    CHECK(t.n == 5, "expected 5 tokens for 'rm -rf /'");
    if (t.n == 5) {
        CHECK(t.v[0].kind == TOK_WORD && str_eq_cstr(t.v[0].text, "rm"), "tok0");
        CHECK(t.v[1].kind == TOK_SEP && str_eq_cstr(t.v[1].text, " "), "tok1");
        CHECK(t.v[2].kind == TOK_WORD && str_eq_cstr(t.v[2].text, "-rf"), "tok2");
        CHECK(t.v[3].kind == TOK_SEP, "tok3");
        CHECK(t.v[4].kind == TOK_WORD && str_eq_cstr(t.v[4].text, "/"), "tok4");
    }
}

static void test_operator_runs_merge(void)
{
    Tokens t;
    memset(&t, 0, sizeof t);
    const char *cmd = "a && b >> c 2>&1";
    lex(cmd, strlen(cmd), &t);
    int has_and = 0, has_append = 0, has_dupfd = 0;
    for (size_t i = 0; i < t.n; i++) {
        if (t.v[i].kind == TOK_OP) {
            if (str_eq_cstr(t.v[i].text, "&&"))
                has_and = 1;
            if (str_eq_cstr(t.v[i].text, ">>"))
                has_append = 1;
            if (str_eq_cstr(t.v[i].text, ">&"))
                has_dupfd = 1;
        }
    }
    CHECK(has_and && has_append && has_dupfd, "operator runs must merge into &&, >>, >&");
    CHECK(reconstruct_eq(&t, cmd, strlen(cmd)), "operator merge must not lose bytes");
}

static void test_quoted_spans(void)
{
    Tokens t;
    memset(&t, 0, sizeof t);
    lex("echo 'a b c' \"d\" `e`", 20, &t);
    int sq = 0, dq = 0, bt = 0;
    for (size_t i = 0; i < t.n; i++) {
        if (t.v[i].kind == TOK_SQUOTE && str_eq_cstr(t.v[i].text, "'a b c'"))
            sq = 1;
        if (t.v[i].kind == TOK_DQUOTE && str_eq_cstr(t.v[i].text, "\"d\""))
            dq = 1;
        if (t.v[i].kind == TOK_BACKTICK && str_eq_cstr(t.v[i].text, "`e`"))
            bt = 1;
    }
    CHECK(sq, "single-quoted span kept whole");
    CHECK(dq, "double-quoted span kept whole");
    CHECK(bt, "backtick span kept whole");
}

static void test_unclosed_quote_flagged(void)
{
    CHECK(check_code("echo 'oops", "bash", "UNCLOSED-QUOTE"), "unclosed quote must be flagged");
}

/* ---------- segments ---------- */

static void test_pipeline_segments(void)
{
    Segments s = segs_of("rg pat | head -5 | wc -l");
    CHECK(s.n == 3, "pipeline must split into 3 segments");
    if (s.n == 3) {
        CHECK(str_eq_cstr(s.v[0].head, "rg"), "head 0");
        CHECK(str_eq_cstr(s.v[1].head, "head"), "head 1");
        CHECK(str_eq_cstr(s.v[2].head, "wc"), "head 2");
        CHECK(seg_pipes_out(&s.v[0]) && seg_pipes_out(&s.v[1]), "pipe terminators");
        CHECK(!seg_pipes_out(&s.v[2]) && s.v[2].terminator == NULL, "last segment unterminated");
    }
}

static void test_segments_own_their_words(void)
{
    Segments s = segs_of("rm -rf / && ls");
    CHECK(s.n == 2, "two segments");
    if (s.n == 2) {
        CHECK(s.v[0].nwords == 3 && str_eq_cstr(s.v[0].words[0], "rm") &&
              str_eq_cstr(s.v[0].words[1], "-rf") && str_eq_cstr(s.v[0].words[2], "/"),
              "segment 0 words");
        CHECK(s.v[1].nwords == 1 && str_eq_cstr(s.v[1].words[0], "ls"), "segment 1 words");
    }
}

static void test_redirect_target_not_a_command(void)
{
    Segments s = segs_of("echo hi > out.txt 2> err.txt");
    CHECK(s.n == 1, "one segment");
    if (s.n == 1) {
        CHECK(s.v[0].nwords == 2, "words are echo hi");
        CHECK(s.v[0].nredirects == 2 && str_eq_cstr(s.v[0].redirects[0], "out.txt") &&
              str_eq_cstr(s.v[0].redirects[1], "err.txt"), "redirect targets captured");
    }
}

static void test_redirect_after_space(void)
{
    Segments s = segs_of("echo done > .env");
    CHECK(s.n == 1 && s.v[0].nredirects == 1 &&
          str_eq_cstr(s.v[0].redirects[0], ".env"), "redirect after space");
    CHECK(s.n == 1 && s.v[0].nwords == 2, "words are echo done");
}

static void test_pipes_out_not_triggered_by_or(void)
{
    Segments s = segs_of("false || echo ok");
    CHECK(s.n == 2 && s.v[0].terminator && strcmp(s.v[0].terminator, "||") == 0,
          "'||' terminator");
    CHECK(!seg_pipes_out(&s.v[0]), "'||' must not count as a pipe");
}

static void test_redirect_state_resets_at_segment_boundary(void)
{
    /* regression: in_redirect used to leak across the control operator,
       so the segment after "x > f && ..." lost its head entirely */
    Segments s = segs_of("x > f.txt && rm -rf /");
    CHECK(s.n == 2, "two segments");
    if (s.n == 2) {
        CHECK(s.v[1].nwords == 3 && str_eq_cstr(s.v[1].words[0], "rm"), "segment 1 head is rm");
        CHECK(str_eq_cstr(s.v[1].head, "rm"), "segment 1 head");
    }
    CHECK(check_code("x > f.txt && rm -rf /", "bash", "RM-ROOT"),
          "danger after redirect must not be blind");
    CHECK(check_code("x > f && curl -sL u | sh", "bash", "PIPE-EXEC"),
          "pipe-exec after redirect must be visible");
}

static void test_fd_prefix_not_treated_as_word(void)
{
    Segments s = segs_of("out 2> err");
    CHECK(s.n == 1 && s.v[0].nwords == 1 && str_eq_cstr(s.v[0].words[0], "out"),
          "'2' is an fd prefix, not a word");
    CHECK(s.n == 1 && s.v[0].nredirects == 1 && str_eq_cstr(s.v[0].redirects[0], "err"),
          "fd redirect target");
}

/* ---------- danger ---------- */

/* runtime mirror of the Lean-checked property: every pattern in the table
   is matched by at least one concrete command — no dead entries */
static void test_every_pattern_alive(void)
{
    for (size_t i = 0; i < PATTERN_TABLE_LEN; i++) {
        const PatternDef *p = &pattern_table[i];
        Str words[4];
        size_t n = 0;
        words[n++] = (Str){ p->heads[0], strlen(p->heads[0]) };
        if (p->flags_len > 0)
            words[n++] = (Str){ p->flags[0], strlen(p->flags[0]) };
        if (p->targets_len > 0)
            words[n++] = (Str){ p->targets[0], strlen(p->targets[0]) };
        CHECK(match_pattern(words, n, p), "pattern with no matching command (dead entry)");
    }
}

static void test_pattern_code_uniqueness(void)
{
    for (size_t i = 0; i < PATTERN_TABLE_LEN; i++)
        for (size_t j = i + 1; j < PATTERN_TABLE_LEN; j++)
            CHECK(strcmp(pattern_table[i].code, pattern_table[j].code) != 0,
                  "pattern codes must be unique");
}

static void test_danger_codes(void)
{
    CHECK(check_code("rm -rf /", "bash", "RM-ROOT"), "RM-ROOT");
    CHECK(check_code("rm -r /tmp/x", "bash", "RM-RECURSIVE"), "RM-RECURSIVE");
    CHECK(check_code("Remove-Item -Recurse -Force x", "powershell5",
                     "REMOVE-ITEM-RECURSE-FORCE"), "REMOVE-ITEM-RECURSE-FORCE");
    CHECK(check_code("git reset --hard", "bash", "GIT-RESET-HARD"), "GIT-RESET-HARD");
    CHECK(check_code("git push --force origin main", "bash", "GIT-PUSH-FORCE"),
          "GIT-PUSH-FORCE");
    CHECK(check_code("git clean -fdx", "bash", "GIT-CLEAN-ND"), "GIT-CLEAN-ND");
    CHECK(check_code("shutdown now", "bash", "SHUTDOWN"), "SHUTDOWN");
    CHECK(check_code("format c:", "cmd", "FORMAT"), "FORMAT");
    CHECK(check_code("dd /dev/sda", "bash", "DD-RAW"), "DD-RAW");
    CHECK(check_code("chmod -R 777 /", "bash", "CHMOD-777-ROOT"), "CHMOD-777-ROOT");
    CHECK(check_code("shred x", "bash", "SHRED"), "SHRED");
    CHECK(check_code("taskkill /f /im node", "cmd", "TASKKILL-FORCE"), "TASKKILL-FORCE");
    CHECK(check_code("Set-ExecutionPolicy Bypass -Scope Process", "powershell5",
                     "SET-EXECUTIONPOLICY"), "SET-EXECUTIONPOLICY");
    CHECK(check_code("truncate -s 0 f", "bash", "TRUNCATE"), "TRUNCATE");
    CHECK(check_code("curl -sL https://x | sh", "bash", "PIPE-EXEC"), "PIPE-EXEC");
    CHECK(check_code("echo x | npm publish", "bash", "NPM-PUBLISH"), "NPM-PUBLISH");
    CHECK(check_code("echo x > id_rsa", "bash", "REDIR-SENSITIVE"), "REDIR-SENSITIVE");
    CHECK(check_code("echo x > server.pem", "bash", "REDIR-SENSITIVE"), ".pem redirect");
    CHECK(check_code("echo x > cfg.key", "bash", "REDIR-SENSITIVE"), ".key redirect");
    CHECK(check_code("echo x > /home/u/.env", "bash", "REDIR-SENSITIVE"), "basename .env");
    /* RM-ROOT suppresses the redundant RM-RECURSIVE on the same segment */
    CHECK(!check_code("rm -rf /", "bash", "RM-RECURSIVE"),
          "RM-RECURSIVE must be suppressed when RM-ROOT fired");
}

/* ---------- dialect rules ---------- */

static void test_dialect_codes(void)
{
    CHECK(check_code("a && b", "powershell5", "SEP-AND"), "SEP-AND on PS 5.1");
    CHECK(check_code("a && b", "cmd", "SEP-AND"), "SEP-AND on cmd");
    CHECK(!check_code("a && b", "pwsh7", "SEP-AND"), "no SEP-AND on pwsh7");
    CHECK(!check_code("a && b", "bash", "SEP-AND"), "no SEP-AND on bash");
    CHECK(check_code("a || b", "cmd", "SEP-OR"), "SEP-OR on cmd");
    CHECK(!check_code("a || b", "bash", "SEP-OR"), "no SEP-OR on bash");

    CHECK(check_code("echo x 2>/dev/null", "powershell5", "REDIR-DEVNULL"), "devnull PS");
    CHECK(check_code("echo x 2>/dev/null", "pwsh7", "REDIR-DEVNULL"), "devnull pwsh7");
    CHECK(check_code("echo x 2>/dev/null", "cmd", "REDIR-DEVNULL"), "devnull cmd");
    CHECK(!check_code("echo x 2>/dev/null", "bash", "REDIR-DEVNULL"), "devnull ok on bash");

    CHECK(check_code("echo $HOME", "powershell5", "ENV-VAR"), "ENV-VAR PS");
    CHECK(check_code("echo $HOME", "cmd", "ENV-VAR"), "ENV-VAR cmd");
    CHECK(!check_code("echo $HOME", "bash", "ENV-VAR"), "no ENV-VAR on bash");
    CHECK(check_code("echo ${HOME}", "powershell5", "BRACE-VAR"), "BRACE-VAR");
    CHECK(check_code("echo $?", "powershell5", "SPECIAL-VAR"), "SPECIAL-VAR");
    CHECK(check_code("export FOO=bar", "powershell5", "EXPORT"), "EXPORT PS");
    CHECK(check_code("export FOO=bar", "cmd", "EXPORT"), "EXPORT cmd");
    CHECK(!check_code("export FOO=bar", "bash", "EXPORT"), "no EXPORT on bash");
    CHECK(check_code("source ./env", "powershell5", "SOURCE"), "SOURCE");
    CHECK(check_code("echo `uname`", "powershell5", "BACKTICK"), "BACKTICK PS");
    CHECK(check_code("echo `uname`", "cmd", "BACKTICK"), "BACKTICK cmd");
    CHECK(!check_code("echo `uname`", "bash", "BACKTICK"), "no BACKTICK on bash");
    CHECK(check_code("curl -sL u", "powershell5", "CURL-ALIAS"), "CURL-ALIAS");
    CHECK(check_code("rm -r x", "powershell5", "RM-FLAGS"), "RM-FLAGS");
    CHECK(check_code("Get-ChildItem -Recurse", "bash", "CMDLET-IN-POSIX"), "CMDLET-IN-POSIX");
    CHECK(!check_code("Get-ChildItem -Recurse", "powershell5", "CMDLET-IN-POSIX"),
          "cmdlet is native on PowerShell");
    CHECK(check_code("grep pat f", "powershell5", "POSIX-CMD"), "POSIX-CMD");
}

/* ---------- tools (controlled PATH) ---------- */

static const char *PATHBIN = "test_pathbin";

static void set_path_env(const char *dir)
{
#if defined(_WIN32)
    _putenv_s("PATH", dir);
#else
    setenv("PATH", dir, 1);
#endif
    shp_tools_reset();
}

static void restore_path_env(const char *old)
{
    if (old)
        set_path_env(old);
    shp_tools_reset();
}

static void write_empty_file(const char *path)
{
    FILE *f = fopen(path, "wb");
    if (f)
        fclose(f);
}

static void test_tools_resolution(void)
{
    const char *old = getenv("PATH");
    MKDIR(PATHBIN);
#if defined(_WIN32)
    write_empty_file("test_pathbin/fakecmd-xyz.exe");
    write_empty_file("test_pathbin/grep.exe");
#else
    write_empty_file("test_pathbin/fakecmd-xyz");
    write_empty_file("test_pathbin/grep");
#endif
    set_path_env(PATHBIN);

    Report r;
    const char *err = NULL;

    memset(&r, 0, sizeof r);
    CHECK(preflight("fakecmd-xyz --v", "bash", 1, &r, &err) == 0, "preflight ok");
    CHECK(r.ntools == 1 && strcmp(r.tools[0].name, "fakecmd-xyz") == 0 &&
          strcmp(r.tools[0].status, "found") == 0 && r.tools[0].path != NULL,
          "tool found on controlled PATH");

    memset(&r, 0, sizeof r);
    CHECK(preflight("no-such-cmd-xyz --v", "bash", 1, &r, &err) == 0, "preflight ok");
    CHECK(r.ntools == 1 && strcmp(r.tools[0].status, "missing") == 0 &&
          r.tools[0].path == NULL, "missing tool reported");
    CHECK(has_code(&r, "TOOL-NOT-FOUND"), "TOOL-NOT-FOUND issue for missing head");

    memset(&r, 0, sizeof r);
    CHECK(preflight("mkdir newdir", "cmd", 1, &r, &err) == 0, "preflight ok");
    CHECK(r.ntools == 0, "builtin heads are not resolved on PATH");

    memset(&r, 0, sizeof r);
    CHECK(preflight("foo --v | foo --v | foo", "bash", 1, &r, &err) == 0, "preflight ok");
    CHECK(r.ntools == 1 && strcmp(r.tools[0].name, "foo") == 0,
          "duplicate heads resolved once");

    /* POSIX-CMD downgrades to info when the tool is actually on PATH */
    memset(&r, 0, sizeof r);
    CHECK(preflight("grep pat f", "powershell5", 1, &r, &err) == 0, "preflight ok");
    const Issue *pc = find_code(&r, "POSIX-CMD");
    CHECK(pc != NULL, "POSIX-CMD present");
    CHECK(pc && strcmp(pc->severity, "info") == 0,
          "POSIX-CMD downgrades to info when found on PATH");
    CHECK(!has_code(&r, "TOOL-NOT-FOUND"), "no TOOL-NOT-FOUND when POSIX-CMD already covers it");

    /* no-path-check skips resolution entirely */
    memset(&r, 0, sizeof r);
    CHECK(preflight("no-such-cmd-xyz --v", "bash", 0, &r, &err) == 0, "preflight ok");
    CHECK(r.ntools == 0, "--no-path-check yields no tools");

    /* 'find' keeps its hard failure even when present on PATH */
#if defined(_WIN32)
    write_empty_file("test_pathbin/find.exe");
#else
    write_empty_file("test_pathbin/find");
#endif
    shp_tools_reset();
    memset(&r, 0, sizeof r);
    CHECK(preflight("find . -name x", "powershell5", 1, &r, &err) == 0, "preflight ok");
    const Issue *fi = find_code(&r, "POSIX-CMD");
    CHECK(fi && strcmp(fi->severity, "error") == 0,
          "'find' is never downgraded (Windows namesake differs)");

    restore_path_env(old);
#if defined(_WIN32)
    remove("test_pathbin/find.exe");
    remove("test_pathbin/fakecmd-xyz.exe");
    remove("test_pathbin/grep.exe");
#else
    remove("test_pathbin/find");
    remove("test_pathbin/fakecmd-xyz");
    remove("test_pathbin/grep");
#endif
    RMDIR(PATHBIN);
}

static void test_builtins_sorted(void)
{
    for (size_t i = 0; i + 1 < BUILTINS_LEN; i++)
        CHECK(strcmp(builtins[i], builtins[i + 1]) < 0,
              "builtins table must stay sorted for bsearch");
    for (size_t i = 0; i < BUILTINS_LEN; i++)
        CHECK(is_builtin(builtins[i]), "bsearch must find every builtin");
    CHECK(!is_builtin("not-a-builtin"), "bsearch must reject unknown names");
}

/* ---------- report ---------- */

static void test_verdicts_and_exit_codes(void)
{
    struct {
        const char *cmd;
        const char *verdict;
        int exit_code;
    } cases[] = {
        { "ls", "ok", 0 },
        { "git clean -fd", "warn", 1 },
        { "rm -rf /", "fail", 2 },
    };
    for (size_t c = 0; c < sizeof cases / sizeof cases[0]; c++) {
        Report r;
        const char *err = NULL;
        memset(&r, 0, sizeof r);
        CHECK(preflight(cases[c].cmd, "bash", 0, &r, &err) == 0, "preflight ok");
        CHECK(strcmp(r.verdict, cases[c].verdict) == 0, "verdict");
        CHECK(report_exit_code(&r) == cases[c].exit_code, "exit code");
    }
}

static void test_json_shape(void)
{
    Report r;
    const char *err = NULL;
    memset(&r, 0, sizeof r);
    CHECK(preflight("rm -rf /", "bash", 0, &r, &err) == 0, "preflight ok");
    Buf b;
    memset(&b, 0, sizeof b);
    report_to_json(&r, &b);
    CHECK(b.n > 0, "json rendered");
    CHECK(strstr(b.p, "\"verdict\": \"fail\"") != NULL, "verdict in json");
    CHECK(strstr(b.p, "\"target\": \"bash\"") != NULL, "target in json");
    CHECK(strstr(b.p, "\"errors\": 1,") != NULL, "errors count in json");
    CHECK(strstr(b.p, "\"code\": \"RM-ROOT\"") != NULL, "issue code in json");
    CHECK(strstr(b.p, "\"fix\":") != NULL, "fix present in json");
    CHECK(strstr(b.p, "\"elapsed_ms\":") != NULL, "elapsed in json");
    CHECK(strstr(b.p, "\"tools\": []") != NULL, "empty tools render as []");

    /* empty-issues report must not render null lists */
    memset(&r, 0, sizeof r);
    CHECK(preflight("ls", "bash", 0, &r, &err) == 0, "preflight ok");
    memset(&b, 0, sizeof b);
    report_to_json(&r, &b);
    CHECK(strstr(b.p, "\"issues\": []") != NULL, "empty issues render as []");
    CHECK(strstr(b.p, "null") == NULL, "no null lists in json");
}

static void test_text_output(void)
{
    Report r;
    const char *err = NULL;
    memset(&r, 0, sizeof r);
    CHECK(preflight("a && b", "powershell5", 0, &r, &err) == 0, "preflight ok");
    Buf b;
    memset(&b, 0, sizeof b);
    report_to_text(&r, &b);
    CHECK(strstr(b.p, "shpreflight: fail (1 error(s), 0 warning(s)) for powershell5\n") != NULL,
          "text header");
    CHECK(strstr(b.p, "  SEP-AND [error] syntax: operator '&&' is not supported in this shell\n") != NULL,
          "issue line");
    CHECK(strstr(b.p, "    fix: separate commands, or chain with ';' if order alone matters\n") != NULL,
          "fix line");

    memset(&r, 0, sizeof r);
    CHECK(preflight("ls", "bash", 0, &r, &err) == 0, "preflight ok");
    memset(&b, 0, sizeof b);
    report_to_text(&r, &b);
    CHECK(strstr(b.p, "  no issues found\n") != NULL, "no-issues line");
}

/* ---------- shells registry ---------- */

static void test_shell_registry(void)
{
    CHECK(SHELL_TABLE_LEN == 5, "five shells registered");
    const char *t = NULL;
    const char *err = NULL;
    CHECK(resolve_target(NULL, &t, &err) == 0, "NULL resolves");
    CHECK(resolve_target("auto", &t, &err) == 0 && t != NULL, "auto resolves");
    CHECK(resolve_target("fish", &t, &err) != 0 && err != NULL, "unknown shell rejected");
    CHECK(resolve_target("bash", &t, &err) == 0 && strcmp(t, "bash") == 0, "bash resolves");
    CHECK(resolve_target("pwsh7", &t, &err) == 0 && strcmp(t, "pwsh7") == 0, "pwsh7 resolves");
}

static void test_shells_output(void)
{
    FILE *f = tmpfile();
    CHECK(f != NULL, "tmpfile for shells output");
    if (!f)
        return;
    run_shells(f);
    rewind(f);
    char buf[4096];
    size_t n = fread(buf, 1, sizeof buf - 1, f);
    buf[n] = '\0';
    fclose(f);
    CHECK(strstr(buf, "\"powershell5\": \"Windows PowerShell 5.1 (powershell.exe)\"") != NULL,
          "powershell5 entry");
    CHECK(strstr(buf, "\"sh\": \"POSIX sh\"") != NULL, "sh entry");
    CHECK(strstr(buf, "\"bash\": \"Bash (Git Bash / WSL / Unix)\"") != NULL, "bash entry");
    CHECK(strstr(buf, "\"cmd\": \"Windows cmd.exe\"") != NULL, "cmd entry");
    CHECK(strstr(buf, "\"pwsh7\": \"PowerShell 7+ (pwsh.exe)\"") != NULL, "pwsh7 entry");
}

/* ---------- CLI argument parsing ---------- */

static void test_parse_check_args(void)
{
    CheckArgs ca;

    {
        char *argv1[] = { "ls", "-la" };
        CHECK(parse_check_args(2, argv1, &ca) == 0, "plain positionals");
        CHECK(ca.npos == 2 && strcmp(ca.pos[0], "ls") == 0, "positionals kept");
        CHECK(strcmp(ca.shell, "auto") == 0 && strcmp(ca.format, "text") == 0, "defaults");
    }
    {
        char *argv2[] = { "--shell", "bash", "--format", "json", "cmd", "-rf" };
        CHECK(parse_check_args(6, argv2, &ca) == 0, "flags before positionals");
        CHECK(strcmp(ca.shell, "bash") == 0 && strcmp(ca.format, "json") == 0, "flag values");
        CHECK(ca.npos == 2, "command words survive");
    }
    {
        char *argv3[] = { "--shell=cmd", "--format=json", "--stdin" };
        CHECK(parse_check_args(3, argv3, &ca) == 0, "equals-form flags");
        CHECK(strcmp(ca.shell, "cmd") == 0 && ca.use_stdin == 1, "equals values");
    }
    {
        /* command words that look like flags must stay positional */
        char *argv4[] = { "rm", "-rf", "--", "--not-a-flag", "x" };
        CHECK(parse_check_args(5, argv4, &ca) == 0, "double dash");
        CHECK(ca.npos == 4 && strcmp(ca.pos[0], "rm") == 0, "everything is positional after --");
    }
    {
        char *argv5[] = { "--format", "bogus" };
        CHECK(parse_check_args(2, argv5, &ca) != 0 && ca.err != NULL, "format validated");
    }
    {
        char *argv6[] = { "--shell" };
        CHECK(parse_check_args(1, argv6, &ca) != 0 && ca.err != NULL, "--shell needs a value");
    }
    {
        char *argv7[] = { "--no-path-check", "x" };
        CHECK(parse_check_args(2, argv7, &ca) == 0 && ca.no_path_check == 1, "no-path-check");
    }
}

/* ---------- CLI end-to-end ---------- */

static char *slurp_tmpfile(FILE *f)
{
    static char buf[8192];
    size_t n;
    rewind(f);
    n = fread(buf, 1, sizeof buf - 1, f);
    buf[n] = '\0';
    fclose(f);
    return buf;
}

static void test_cli_run(void)
{
    {
        char *argv[] = { "check", "--shell", "bash", "--format", "json",
                         "--no-path-check", "rm", "-rf", "/" };
        FILE *out = tmpfile();
        int rc = run(9, argv, out, stderr);
        CHECK(rc == 2, "failing command exits 2");
        char *s = slurp_tmpfile(out);
        CHECK(strstr(s, "\"verdict\": \"fail\"") != NULL, "json verdict via CLI");
        CHECK(strstr(s, "\"code\": \"RM-ROOT\"") != NULL, "RM-ROOT via CLI");
    }
    {
        char *argv[] = { "check", "--format=text", "--shell=bash",
                         "--no-path-check", "a && b" };
        FILE *out = tmpfile();
        int rc = run(5, argv, out, stderr);
        CHECK(rc == 0, "&& is fine on bash");
        char *s = slurp_tmpfile(out);
        CHECK(strstr(s, "shpreflight: ok") != NULL, "text output via CLI");
    }
    {
        char *argv[] = { "check", "--shell", "powershell5", "a && b" };
        FILE *out = tmpfile();
        int rc = run(5, argv, out, stderr);
        CHECK(rc == 2, "&& breaks powershell5");
        char *s = slurp_tmpfile(out);
        CHECK(strstr(s, "SEP-AND") != NULL, "SEP-AND via CLI");
    }
    {
        char *argv[] = { "check" };
        FILE *errf = tmpfile();
        int rc = run(1, argv, tmpfile(), errf);
        CHECK(rc == 3, "no command given exits 3");
        char *s = slurp_tmpfile(errf);
        CHECK(strstr(s, "error: no command given") != NULL, "no-command message");
    }
    {
        char *argv[] = { "check", "--shell", "fish", "ls" };
        FILE *errf = tmpfile();
        int rc = run(4, argv, tmpfile(), errf);
        CHECK(rc == 3, "unknown shell exits 3");
        char *s = slurp_tmpfile(errf);
        CHECK(strstr(s, "unknown shell 'fish'") != NULL, "unknown shell message");
    }
    {
        char *argv[] = { "check", "--format", "bogus", "ls" };
        FILE *errf = tmpfile();
        int rc = run(4, argv, tmpfile(), errf);
        CHECK(rc == 3, "bad format exits 3");
    }
    {
        char *argv[] = { "shells" };
        FILE *out = tmpfile();
        int rc = run(1, argv, out, stderr);
        CHECK(rc == 0, "shells exits 0");
        char *s = slurp_tmpfile(out);
        CHECK(strstr(s, "\"powershell5\"") != NULL && strstr(s, "\"sh\"") != NULL,
              "shells listing via CLI");
    }
    {
        char *argv[] = { "--help" };
        FILE *out = tmpfile();
        int rc = run(1, argv, out, stderr);
        CHECK(rc == 0, "help exits 0");
        char *s = slurp_tmpfile(out);
        CHECK(strstr(s, "usage: shpreflight <command> [options]") != NULL, "usage text");
    }
    {
        char *argv[] = { "bogus-subcommand" };
        FILE *errf = tmpfile();
        int rc = run(1, argv, tmpfile(), errf);
        CHECK(rc == 3, "unknown subcommand exits 3");
    }
}

/* ---------- main ---------- */

int main(void)
{
    test_lexer_reconstruct();
    test_word_split();
    test_operator_runs_merge();
    test_quoted_spans();
    test_unclosed_quote_flagged();
    test_pipeline_segments();
    test_segments_own_their_words();
    test_redirect_target_not_a_command();
    test_redirect_after_space();
    test_pipes_out_not_triggered_by_or();
    test_redirect_state_resets_at_segment_boundary();
    test_fd_prefix_not_treated_as_word();
    test_every_pattern_alive();
    test_pattern_code_uniqueness();
    test_danger_codes();
    test_dialect_codes();
    test_tools_resolution();
    test_builtins_sorted();
    test_verdicts_and_exit_codes();
    test_json_shape();
    test_text_output();
    test_shell_registry();
    test_shells_output();
    test_parse_check_args();
    test_cli_run();

    printf("%d checks, %d failures\n", checks, failures);
    return failures == 0 ? 0 : 1;
}

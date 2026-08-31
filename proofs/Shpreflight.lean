import Patterns

/-! # Machine-checked properties of shpreflight

Three properties that make the tool trustworthy are proved here in plain
Lean 4 core (no Mathlib) against the generated runtime table in
`Patterns.lean` (exported verbatim from the Python implementation, so
proofs cannot drift from code):

1. `reconstruct_lexer` — the lexer loses no characters: the concatenation
   of all token texts equals the input string exactly. A destructive
   fragment can never hide in a lexing blind spot.
2. `all_patterns_alive` — every entry in the danger table is matchable:
   known-dangerous commands can never be silently unmatchable.
3. `patternCodes_nodup` — table codes are unique, so report consumers can
   key on them.

Run:  powershell -ExecutionPolicy Bypass -File proofs\verify_proofs.ps1
-/

set_option maxRecDepth 4000

/-! ## 1. Lexer -/

inductive TokKind where
  | word | sep | op | squote | dquote | backtick
  deriving Repr

def isOpChar (c : Char) : Bool := c ∈ ['&', '|', ';', '<', '>']
def isWs (c : Char) : Bool := c ∈ [' ', '\t', '\r', '\n']
def isQuote (c : Char) : Bool := c = '\'' || c = '"' || c = '`'
def isWordStop (c : Char) : Bool := isOpChar c || isWs c || isQuote c
def isWordChar (c : Char) : Bool := !isWordStop c

def quoteKind : Char → TokKind
  | '\'' => .squote
  | '"' => .dquote
  | _ => .backtick

/-- Longest prefix of `l` whose characters satisfy `p`. -/
def takeRun (p : Char → Bool) : List Char → List Char
  | [] => []
  | c :: cs => if p c then c :: takeRun p cs else []

/-- Remainder after removing the longest prefix satisfying `p`. -/
def dropRun (p : Char → Bool) : List Char → List Char
  | [] => []
  | c :: cs => if p c then dropRun p cs else c :: cs

theorem takeRun_cons_true {p : Char → Bool} {c : Char} {cs : List Char}
    (h : p c = true) : takeRun p (c :: cs) = c :: takeRun p cs := by
  simp [takeRun, h]

theorem dropRun_cons_true {p : Char → Bool} {c : Char} {cs : List Char}
    (h : p c = true) : dropRun p (c :: cs) = dropRun p cs := by
  simp [dropRun, h]

theorem run_append (p : Char → Bool) : ∀ l : List Char,
    takeRun p l ++ dropRun p l = l := by
  intro l
  induction l with
  | nil => rfl
  | cons c cs ih =>
    by_cases h : p c = true
    · rw [takeRun_cons_true h, dropRun_cons_true h]
      exact congrArg (List.cons c) ih
    · simp [takeRun, dropRun, h]

theorem dropRun_le (p : Char → Bool) : ∀ l : List Char,
    (dropRun p l).length ≤ l.length := by
  intro l
  induction l with
  | nil => simp [dropRun]
  | cons c cs ih =>
    by_cases h : p c = true
    · rw [dropRun_cons_true h]
      simp only [List.length_cons]
      omega
    · simp [dropRun, h]

/-- `spanUntil q cs = some (before, after)` exactly when
    `cs = before ++ q :: after`; `none` when `q` never occurs in `cs`. -/
def spanUntil (q : Char) : List Char → Option (List Char × List Char)
  | [] => none
  | c :: cs =>
    if c = q then some ([], cs)
    else
      match spanUntil q cs with
      | none => none
      | some (a, b) => some (c :: a, b)

theorem spanUntil_spec {q : Char} :
    ∀ {cs a b}, spanUntil q cs = some (a, b) → cs = a ++ q :: b := by
  intro cs
  induction cs with
  | nil => intro a b h; cases h
  | cons c cs ih =>
    intro a b h
    unfold spanUntil at h
    split at h
    · next hq =>
      obtain ⟨rfl, rfl⟩ := Option.some.inj h
      subst hq
      simp
    · next hq =>
      split at h
      · cases h
      · next inner rest heq =>
        obtain ⟨rfl, rfl⟩ := Option.some.inj h
        rw [ih heq]
        rfl

theorem spanUntil_length {q : Char} :
    ∀ {cs a b}, spanUntil q cs = some (a, b) → b.length ≤ cs.length := by
  intro cs
  induction cs with
  | nil => intro a b h; cases h
  | cons c cs ih =>
    intro a b h
    unfold spanUntil at h
    split at h
    · next hq => obtain ⟨rfl, rfl⟩ := Option.some.inj h; simp
    · next hq =>
      split at h
      · cases h
      · next inner rest heq =>
        obtain ⟨rfl, rfl⟩ := Option.some.inj h
        have hb := ih heq
        simp only [List.length_cons]
        omega

/-! ### The lexer, mirroring shpreflight.lex step for step

Structural recursion on explicit fuel (the input length) keeps the
definition executable and makes conservation a plain induction. -/

def lexChrs : Nat → List Char → List (TokKind × String)
  | 0, _ => []
  | _ + 1, [] => []
  | n + 1, c :: cs =>
    if isWs c then
      (TokKind.sep, String.mk (takeRun isWs (c :: cs))) ::
        lexChrs n (dropRun isWs (c :: cs))
    else if isQuote c then
      match spanUntil c cs with
      | none => [(quoteKind c, String.mk (c :: cs))]
      | some (inner, rest) =>
          (quoteKind c, String.mk (c :: inner ++ [c])) :: lexChrs n rest
    else if isOpChar c then
      (TokKind.op, String.mk (takeRun isOpChar (c :: cs))) ::
        lexChrs n (dropRun isOpChar (c :: cs))
    else
      (TokKind.word, String.mk (takeRun isWordChar (c :: cs))) ::
        lexChrs n (dropRun isWordChar (c :: cs))

def lex (cmd : String) : List (TokKind × String) :=
  lexChrs cmd.data.length cmd.data

def tokTexts : List (TokKind × String) → List Char :=
  fun ts => ts.foldr (fun t acc => t.2.data ++ acc) []

theorem tokTexts_nil : tokTexts [] = [] := rfl

theorem tokTexts_cons (k : TokKind) (text : String)
    (ts : List (TokKind × String)) :
    tokTexts ((k, text) :: ts) = text.data ++ tokTexts ts := rfl

theorem data_mk (l : List Char) : (String.mk l).data = l := rfl

/-! ### Conservation: lexing loses nothing -/

theorem lexChrs_conserves : ∀ (n : Nat) (s : List Char),
    s.length ≤ n → tokTexts (lexChrs n s) = s := by
  intro n
  induction n with
  | zero =>
    intro s hs
    match s with
    | [] => rfl
    | c :: cs =>
      simp only [List.length_cons] at hs
      exact absurd hs (by omega)
  | succ n ih =>
    intro s hs
    match s with
    | [] => rfl
    | c :: cs =>
      have hcs : cs.length ≤ n := by
        simp only [List.length_cons] at hs; omega
      simp only [lexChrs]
      split
      · next hw =>
        have ihi := ih (dropRun isWs (c :: cs))
          (by
            rw [dropRun_cons_true hw]
            have := dropRun_le isWs cs
            omega)
        rw [tokTexts_cons, data_mk, ihi]
        exact run_append isWs (c :: cs)
      · next hw =>
        split
        · next hq =>
          split
          · next heq =>
            rw [tokTexts_cons, data_mk, tokTexts_nil]
            simp
          · next inner rest heq =>
            have ihi := ih rest
              (by
                have := spanUntil_length heq
                omega)
            rw [tokTexts_cons, data_mk, ihi, spanUntil_spec heq]
            simp
        · next hq =>
          split
          · next hop =>
            have ihi := ih (dropRun isOpChar (c :: cs))
              (by
                rw [dropRun_cons_true hop]
                have := dropRun_le isOpChar cs
                omega)
            rw [tokTexts_cons, data_mk, ihi]
            exact run_append isOpChar (c :: cs)
          · next hop =>
            have hwc : isWordChar c = true := by
              simp [isWordChar, isWordStop, hw, hq, hop]
            have ihi := ih (dropRun isWordChar (c :: cs))
              (by
                rw [dropRun_cons_true hwc]
                have := dropRun_le isWordChar cs
                omega)
            rw [tokTexts_cons, data_mk, ihi]
            exact run_append isWordChar (c :: cs)

/-- The main lexer property: concatenating all token texts reproduces the
    input string character for character. -/
theorem reconstruct_lexer (cmd : String) :
    tokTexts (lex cmd) = cmd.data :=
  lexChrs_conserves cmd.data.length cmd.data (Nat.le_refl _)

/-! ## 2. Danger table: no dead entries, no duplicate codes -/

def matchPattern (words : List String) (p : Pattern) : Bool :=
  match words with
  | [] => false
  | w :: rest =>
    p.heads.contains w &&
    (p.flags.isEmpty || p.flags.any (fun f => rest.contains f)) &&
    (p.targets.isEmpty || p.targets.any (fun t => rest.contains t))

/-- A concrete command that triggers pattern `p` for every possible shape
    of the table entry. -/
def witness (p : Pattern) : List String :=
  match p.flags, p.targets with
  | [], [] => [p.heads.head!]
  | f :: _, [] => [p.heads.head!, f]
  | [], t :: _ => [p.heads.head!, t]
  | f :: _, t :: _ => [p.heads.head!, f, t]

/-- Every pattern in the generated runtime table is matchable: the
    detector has no dead entries, so a known-dangerous command can never
    be silently unmatchable. Verified by kernel evaluation over the
    exported table. -/
theorem all_patterns_alive :
    patterns.all (fun p => matchPattern (witness p) p) = true := by decide

/-- Report consumers key on issue codes; the table never emits a code
    twice. -/
theorem patternCodes_nodup : patternCodes.Nodup := by decide

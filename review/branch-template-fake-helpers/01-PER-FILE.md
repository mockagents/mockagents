# Per-File Findings (Pass 1) — feature/template-fake-helpers

_Each file judged in isolation. Cross-file issues live in `02-INTEGRATION.md`._

## `internal/engine/response_generator.go`  ·  Go, +38 LOC  ·  role: engine / response generation

| ID | Severity | Conf | Priority | Effort | Line | Dimension | Evidence → Fix |
|----|----------|------|----------|--------|------|-----------|----------------|
| F-001 | S2 | High | P2 | S | :243–248 | Correctness (UTF-8) | `strings.ToUpper(w[:1]) + strings.ToLower(w[1:])` slices by **byte**, not rune. For a word with a multibyte first rune, `w[:1]` is a lone lead byte and `w[1:]` starts on a continuation byte; Go's `ToUpper`/`ToLower` decode each as `RuneError` and emit U+FFFD — **verified:** `title("über")` → `"��ber"` (the `ü` is destroyed). → Decode the first rune with `utf8.DecodeRuneInString(w)` and upper-case only that rune: `r, sz := utf8.DecodeRuneInString(w); words[i] = string(unicode.ToUpper(r)) + strings.ToLower(w[sz:])`. |
| F-002 | S3 | High | P3 | S | :241–242 | Readability (doc accuracy) | Doc comment says "upper-cases the first letter of each whitespace-separated word" but, via `strings.Fields`, the function also **collapses internal whitespace and trims** — `title("a   b\n")` → `"A B"`. → Note the whitespace-normalizing behavior in the comment (or switch to `strings.Map`/manual scan if preserving spacing matters). |

_`fakePhone`, `fakeCompany`, `fakeUsername`, and the two new package-level slices were reviewed: range bounds on `randomInt` are correct, `%04d` formatting is safe, the 555 exchange is the right reserved range, and the var-block style matches the existing `fakeFirstNames`/`fakeLastNames` pattern. No findings._

## `internal/engine/response_generator_test.go`  ·  Go test, +29 LOC  ·  role: engine / unit tests

| ID | Severity | Conf | Priority | Effort | Line | Dimension | Evidence → Fix |
|----|----------|------|----------|--------|------|-----------|----------------|
| F-003 | S3 | High | P3 | S | ~:133–139 | Tests (weak assertion) | `fake_company` case asserts only `assert.NotEqual(t, "C: ", c)` — a regression returning a single word or an empty suffix would still pass. → Pin the shape: `assert.Regexp(t, \`^C: \w+ \w+$\`, c)`. |
| F-004 | S2 | High | P2 | S | ~:119–123 | Tests (coverage gap / absence) | The `title` case tests only ASCII (`"hello WORLD"` → `"Hello World"`). No case covers non-ASCII (the F-001 break), empty string, or multi-space collapsing. The untested branch is exactly where the bug lives. → Add `title` cases for `"über"` (after F-001 fix, expect `"Über"`), `""` → `""`, and `"a   b"` → `"A B"`. |

_The `title`, `fake_phone`, and `fake_username` happy-path cases are well-formed: the `fake_phone` and `fake_username` regexes pin the output shape and can actually fail. No findings on those._

# Plan and checkpoint brief

## The plan

This is the text that goes into the ticket as `## Agent plan` once approved, so write it for a reader who sees only the ticket. Plain short sentences, GLOSSARY.md terms; explain any other term in one clause.

```markdown
## Agent plan

**Acceptance criteria**
- [ ] AC1: <observable behaviour, checkable by a test or a person>
- [ ] AC2: ...

**Approach**
<What changes and why this way, in a few sentences. Name the modules. When an
alternative was rejected, say which and why in one line.>

**Tests**
| AC | Seam | Test |
| --- | --- | --- |
| AC1 | <service / API (internal/apitest) / repository / e2e> | <test name or file, red first for a bug> |

**PR stack**
1. `<type>/pp-<n>-<slug>` → main: <what it does>; covers AC1, AC2
2. `<type>/pp-<n>-<slug>-<part>` → PR 1: ...

**Out of scope**
- <thing someone might expect, and where it goes instead>

**Follow-ups**
- <problem found on the way, to file as its own ticket>

**Merge danger**
<one-way or two-way door, and the blast radius: migrations, API contracts, UI, config>
```

Rules for the content:

- An acceptance criterion describes behaviour a user or caller can observe ("an unreachable host reports DOWN"), never an implementation step ("add a timeout field").
- Every ticket requirement maps to at least one criterion, or to Out of scope with a reason.
- One PR is a fine stack. Split only when the slices review better apart: a refactor before the feature, backend before UI, a migration before the code that reads it. Each PR passes the full local run on its own.

## The brief

The user runs several sessions at once and hasn't seen your research. The brief gives them what they need to decide in one read. Print it as text, then ask the questions.

```markdown
**PP-<n>: <title>**

**Context.** <What the ticket asks for and why, in 2 or 3 sentences.>

**What I did.**
1. <Step: read X, reproduced Y with Z, traced W>
2. ...

**What I found.**
- <Fact that shapes the plan, with file:line when it helps>
- ...

**Plan.** <The acceptance criteria and the PR stack, short form. Full plan below.>

<the full plan>

**Decisions for you.**
1. <Question>. I recommend <option> because <reason>.
```

Each decision becomes one `AskUserQuestion` question with the recommended option first, labelled "(Recommended)". The tool takes at most 4 questions per call, so batch them across calls. The approval question ("Approve" / "Change something") comes last, in its own call when the decisions changed the plan.

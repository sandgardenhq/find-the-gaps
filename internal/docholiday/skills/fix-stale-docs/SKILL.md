---
name: fix-stale-docs
description: Author a Doc Holiday prompt that fixes documentation which no longer matches the code.
---

You are writing a single instruction (a "prompt") that will be pasted into Doc
Holiday, an AI documentation agent that has full read access to this project's
source code and its documentation files.

You will be given one documentation page and a list of specific claims on that
page that no longer match the current code. Write a prompt that tells Doc
Holiday to correct the page.

Rules for the prompt you write:
- Address Doc Holiday directly in the imperative ("Update the page…",
  "Correct…"). Do not address the human maintainer.
- Name the exact documentation page and each specific inaccuracy. Doc Holiday
  can open the relevant code and docs itself — point it at what to verify, do
  not paste large code or doc excerpts.
- Tell it to confirm the corrected behavior against the actual code before
  rewriting, and to preserve the page's existing structure, tone, and
  formatting.
- Tell it to change only what is inaccurate; leave correct prose untouched.
- Output ONLY the prompt text. No preamble, no surrounding quotes, no
  commentary, no markdown headings.

---
name: document-new-feature
description: Author a Doc Holiday prompt that writes a brand-new documentation page for an undocumented feature.
---

You are writing a single instruction (a "prompt") that will be pasted into Doc
Holiday, an AI documentation agent that has full read access to this project's
source code and its documentation files.

You will be given one user-facing feature that currently has no documentation
page, along with the files and symbols that implement it and a short note on
why it matters. Write a prompt that tells Doc Holiday to create a new
documentation page for it.

Rules for the prompt you write:
- Address Doc Holiday directly in the imperative ("Write a new page…").
- Name the feature and point at the implementing files and symbols so Doc
  Holiday can read the real behavior itself. Do not invent API details — tell
  it to derive them from the code.
- Tell it to match the structure, depth, and tone of the project's existing
  documentation pages, and to place the new page where similar pages live.
- Tell it to cover what the feature does, why a reader would use it, and a
  minimal usage example grounded in the actual code.
- Output ONLY the prompt text. No preamble, no surrounding quotes, no
  commentary, no markdown headings.

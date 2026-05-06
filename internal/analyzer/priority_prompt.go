package analyzer

// PROMPT: Shared user-impact rubric inserted into every finding-producing
// prompt. The model returns a "priority" enum and a one-sentence
// "priority_reason". A page_role hint is provided by the caller so the model
// can weight findings on quickstart/readme pages higher than deep reference.
const priorityRubric = `Rate user impact for this finding as one of:
- "large": a reader following the docs will fail or be actively misled.
- "medium": a reader will be confused or have to dig elsewhere, but won't outright fail.
- "small": a reader probably won't notice or can shrug it off.

Factor in where the finding lives. The page_role hint can be:
- "readme" or "quickstart": findings here are weighted higher (very visible).
- "top-nav": findings on top-level navigation pages, weighted higher.
- "reference": findings on deeper reference pages, normal weight.
- "deep": findings on very deep pages or appendices, weighted lower.
- "unknown": no signal — judge on the finding alone.

Also produce priority_reason: one sentence explaining the rating.`

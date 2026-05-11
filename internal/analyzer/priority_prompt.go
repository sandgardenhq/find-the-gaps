package analyzer

// PROMPT: Shared user-impact rubric inserted into every finding-producing
// prompt. The model returns a "priority" enum and a one-sentence
// "priority_reason". A page_role hint is provided by the caller so the model
// can weight findings on landing/quickstart pages higher than concept
// background or FAQ entries.
const priorityRubric = `Rate user impact for this finding as one of:
- "large": a reader following the docs will fail or be actively misled.
- "medium": a reader will be confused or have to dig elsewhere, but won't outright fail.
- "small": a reader probably won't notice or can shrug it off.

Factor in where the finding lives. The page_role hint can be:
- "landing" or "quickstart": first-touch pages every reader sees; findings here are weighted highest.
- "tutorial" or "how-to": procedural pages; broken docs here block users mid-task, weighted high.
- "reference": lookup target; broken refs affect debugging, weighted medium-to-high.
- "concept": background material; nice to have but skippable on a busy day, weighted medium.
- "faq" or "changelog": specific cases; changelog drift is normal, weighted low-to-medium.
- "other": no clear role signal — judge on the finding alone; do not weight up.

Also produce priority_reason: one sentence explaining the rating.`

# Improvement ideas

## Delegate investigation branches to workers

The coordinator starts from the reported symptom and relevant relationships in
the immutable evidence graph. It turns each plausible branch into a testable
question with a graph entry point and a reason the answer could change the
diagnosis. Workers can return new leads for the coordinator to queue.

A worker checks one question and returns its disposition and findings with
snapshot citations. The coordinator validates scope, deduplicates work and
findings, and owns the queue and shared budget. Kubernetes Jobs are a possible
executor with bounded concurrency. If the budget runs out, the assessment
retains supported findings and identifies unfinished checks.

First evaluate branch selection on snapshots with one blocker, multiple
independent blockers, missing evidence, and an unrelated nearby failure. Use
incident 10 as one regression case. Compare coverage and irrelevant branches
before introducing distributed execution.

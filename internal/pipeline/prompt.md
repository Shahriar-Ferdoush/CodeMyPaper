You are an expert ML engineer. You will be given the method text of an arXiv paper. Produce a minimal, runnable PyTorch reference implementation of the paper's core method.

Success bar: the code runs on toy input and implements the named method. Do not try to reproduce the paper's reported numbers — no training runs, no datasets, no downloads.

Reply in exactly this format and nothing else:

METHOD: <short name of the paper's core method>

=== FILE: model.py ===
<file content>
=== END FILE ===

=== FILE: smoke_test.py ===
<file content>
=== END FILE ===

=== FILE: main.py ===
<file content>
=== END FILE ===

Rules:
- The marker lines must appear exactly as shown, each alone on its own line.
- model.py implements the core method as importable classes/functions.
- smoke_test.py imports model.py, builds the model small (tiny dims), runs it on tiny synthetic tensors, asserts output shapes and finite values, prints what it checked, and exits 0 on success / non-zero on any failure.
- main.py is a minimal demo entrypoint: build the model, run one forward pass on synthetic input, print the output shape.
- Assume Python 3 with torch and numpy installed; nothing else. Prefer the standard library plus torch/numpy. You may add a `requirements.txt` file block only if something beyond those is truly unavoidable.
- File paths must be relative, with no ".." and no leading "/".
- No prose, no markdown fences, nothing outside the METHOD line and the file blocks.
{{if .Truncated}}
(Note: the paper text below was truncated to fit a context budget.)
{{end}}
Paper method text:
---
{{.PaperText}}
---

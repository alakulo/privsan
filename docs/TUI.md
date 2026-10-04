# TUI guide

Open the local review workspace with a file or directory:

~~~sh
privsan tui ./documents
privsan tui --dry-run ./documents
privsan tui --output ../sanitized-new ./documents
privsan tui --in-place --backup-dir ../private-backups ./documents
~~~

Put flags before the input path. Scan options and explicit policy files work as
described in the [CLI reference](CLI.md). The interface requires a real terminal.

## Review

The workspace starts scanning asynchronously. Counters show scanned files,
findings, selected findings, issues and skipped files. The interface uses the
terminal's light or dark background when the terminal reports it.

- **Files:** focus with `f` or `Tab`. Navigate to a file to limit the finding
  list to that file; choose “全部文件” (all files) to restore the full scope.
  `Enter` focuses findings. Files without findings can also be previewed.
- **Findings:** navigate with arrows or `j` / `k`; `Space` toggles the current
  finding. In the file pane, `Space` toggles that file's filtered findings.
  `a` toggles every finding in the current file/search/rule scope.
- **Safe preview:** `Tab` focuses the preview. Use arrows, `j` / `k`,
  `PgUp` / `PgDn`, or `Home` / `End` to scroll vertically. Use `←` / `→`
  or `h` / `l` to scroll long lines horizontally.

Previews mask **all detected values**, including deselected findings. Selection
controls actual replacements: deselected values remain in the exported or
modified file. Undetected sensitive information may remain; detection is an
aid, not a complete security audit.

## Custom replacement workspace

Press `c` to open the **find / replace with** form. Type the search text, use
`Tab` or `Shift+Tab` to move between fields, and enter the replacement. An empty
replacement deletes matches. `Enter` submits the plan for an asynchronous scan;
`Esc` cancels the draft without changing the current review.

| Form key | Action |
|---|---|
| `Ctrl+R` | Toggle literal / Go regular expression search |
| `Ctrl+G` | Toggle case-sensitive / Unicode case-insensitive search |
| `Ctrl+S` | Toggle current file / all included files, when a file is available |
| `Tab` / `Shift+Tab` | Switch input fields |
| `Enter` / `Esc` | Preview the plan / cancel the draft |

To replace in one file, focus **Files** with `f`, select that file and press `c`.
From the all-files scope, the form defaults to all included files. The scope is
shown before submission. All-files replacement follows include/exclude/ignore
rules, independently of the review's path/rule filter. A new plan resets findings
and selections; replacements are not chained onto an unwritten preview. Editing
an existing plan retains its file scope unless another file is selected.

The toolbar, counters and write confirmation identify **custom replacement**.
Use `Space` / `a` to select replacements, `d` to set a new export directory and
`w` to review the write summary. Dry-run and mandatory in-place backups still
apply. `Ctrl+D` returns to privacy redaction and starts a fresh scan. Changing
the input root with `o` resets the replacement file scope to all included files.

The safe preview shows **all replacement candidates**, including deselected
ones, and additionally masks privacy findings from both the source and the
proposed result. A partial phone/email replacement therefore cannot reveal the
rest of that identified value. Privacy preview masks are **not written** in
custom replacement mode: only selected custom replacements affect output.
The confirmation states this distinction. Review retained data before sharing.

The TUI form accepts single-line text and rejects control-character or multiline
pastes; use the CLI for actual newlines or tabs. Find and replacement are each
limited to 4,096 UTF-8 bytes. Replacement is literal, including `$1` and `\n`.
See the [CLI reference](CLI.md#custom-find-and-replace) for limits and CSV rules.
The form adapts to the minimum 48 × 15 terminal size without hiding its controls.

## Filter

`/` opens live search by path or rule ID. `Enter` keeps the search; `Esc` cancels
the edit and restores the previous search. `[` and `]` cycle detected rules.
Search and rule filters can be combined. On the main screen, `Esc` clears search,
rule and file filters.

Filtering never clears previous selections. The write summary includes selections
outside the visible scope. To change those selections, return to all files and
clear the filters.

## Write

`d` edits the export destination. It must be a new directory outside the input
root. `w` opens the write summary, including selected and retained counts, input
path, and export or backup directory. Long summaries scroll with navigation keys;
the action buttons remain visible.

If the export path is wrong, press `d` **inside the confirmation window** to
edit it. Use arrows or `Home` / `End` to position the cursor and `Backspace` to
delete text. `Enter` updates the directory and returns to the refreshed summary;
`Esc` keeps the original directory and returns to confirmation. Both return with
**Cancel** selected, so editing never executes a write. In-place confirmation
does not offer this export-directory action.

Confirmation starts on **Cancel**. `Tab` switches the action, `Enter` activates
it, `y` executes explicitly, and `Esc` or `n` cancels. No write is allowed in
dry-run mode, after an incomplete scan, or with no findings selected.

File I/O remains asynchronous. `q`, `Esc`, or `Ctrl+C` during scanning or writing
requests cancellation and waits for the current file operation to finish.

`e` opens results, operation/recovery directories, errors and skip reasons.
After a write completes, selections are locked; `r` rescans before another write.
`o` changes the input path and starts a fresh scan. `?` shows all keyboard controls.

## Terminal size

The minimum size is **48 columns × 15 rows**; **120 × 30** or larger is
recommended. At 100 columns, the file sidebar appears beside the review area.
Narrower terminals switch between files and findings according to focus.
Shorter terminals show findings or preview according to focus.

The layout measures terminal cells and escapes control characters in file
content, paths and diagnostics. Emoji fonts are not required.

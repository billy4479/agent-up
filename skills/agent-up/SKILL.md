---
name: agent-up
description: Use when you need to upload or share a local file, generated report, HTML page, image, or directory with the user.
---

# Agent Up

Use the `agent-up` CLI to make a local artifact available to the user.

## Safety

- Upload only artifacts that the user requested or that are useful for the current task.
- Do not upload secrets, credentials, private keys, environment files, or unrelated user data.
- Reject symlinks rather than replacing them with their targets.

## Upload A File

Pass the local path directly:

```sh
agent-up ./path/to/report.pdf
```

The command prints exactly one public URL to stdout. Return that URL to the user.

For a file with an incorrect or unknown extension, provide the exact MIME type:

```sh
agent-up ./path/to/output --mime text/plain
```

Do not provide `--name` for a file path. Its basename is used automatically.

## Upload HTML

Name a standalone HTML entry point `index.html` before uploading it:

```sh
agent-up ./path/to/index.html
```

The returned URL renders the HTML directly at the upload root. If the HTML references local CSS, JavaScript, images, or other pages, put all assets in one directory and upload the directory instead.

## Upload A Directory

Upload a directory recursively:

```sh
agent-up ./path/to/site-or-results
```

Directories are browsable. An `index.html` inside any directory is rendered automatically; otherwise the user sees a file listing.

Do not use `--name` or `--mime` with directory uploads. Directory uploads reject symlinks and non-regular entries.

## Upload From Stdin

Use stdin only when there is no suitable file path. `--name` is required and should include a useful extension:

```sh
generate-report | agent-up - --name report.txt --mime text/plain
generate-image | agent-up - --name image.jpg --mime image/jpeg
```

## Handling Results

1. Run one `agent-up` command for each artifact the user needs.
2. Capture the URL printed to stdout without modifying it.
3. Give the user a short description followed by the URL.
4. Do not claim the upload succeeded unless the command exits successfully and prints a URL.

Example response:

```text
Generated report: https://agent-up.example/AbCd1234/report.pdf
```

## Handling Errors

The CLI writes a concise explanation to stderr.

Never substitute another public upload service when `agent-up` fails unless the user explicitly requests it.

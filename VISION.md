# Vision

`llm-filesystem-axi` exists so that an agent has safe, truthful hands on local files without wasting its context or guessing what happened.
It serves agents first, and the people who run them second.
It reads, writes and edits files, searches them by name and content, and manages files and archives through one small binary with token-dense AXI output.
It owns exactly one thing: safe, truthful local file operations.

## Nothing reports success when it did not succeed

This rule sits above every other rule here.
A command that skipped, overwrote or refused part of its work says so, with what and why, in structured errors.
An edit that matched nothing and an archive entry that was skipped are reported, never folded into a plain success.
A missing search path is an error, not "no matches".
An empty result and a failed command are never reported the same way.
A destructive action requires `--confirm`, and a dry run says it is one.
A truncated read says how to resume.
A test asserts the effect on disk, not the exit code.

## Safe by default

Writes stay inside the paths the caller names; an archive entry that escapes its target directory is refused and reported.
Existing files are not overwritten silently; an overwrite is either refused or reported.
An edit never changes a match it was not sure of, and its write never leaves a half-written file or changes the file's mode.
Destructive commands state exactly what they changed.

## The CLI is the product

`llm-filesystem` is the primary surface; an agent that can run a command can run this.
A thin MCP server, `llm-filesystem-mcp`, ships beside it as a sibling fallback for hosts that only speak MCP, and every tool call runs the CLI itself.
Output follows the AXI principles, and TOON through `go-axi` is the format given to a model.
A bare run answers with live data about where the agent is, not a usage screen.
Setup for an agent host works from the installed binary alone, with no clone.

## Small, fast and local

It ships as static Go binaries with no runtime to install.
It never calls a model and never uses the network.
Startup time and output size are measured, because an agent pays for both on every call.

## Scope

It is not a file manager UI, a search index, a sync tool or a version control system.
Code symbols, rename and references belong to `llm-semantic-axi`, project facts and config to `llm-data-axi`, and the network and models to `llm-extract-axi`.
There is no daemon and no file watcher.
Install and upgrade are one command, verified by checksum, on Linux and macOS; Windows is best effort.
Plans and debt live in a private planning repo.

A change aligns when it makes file work faster, safer, more truthful about what happened, or cheaper in tokens.
A change should be resisted when it hides a partial failure, calls a model or the network, adds a heavy dependency, or moves work here that belongs to a sibling tool.

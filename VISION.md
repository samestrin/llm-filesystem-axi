# Vision

`llm-filesystem-axi` exists so that an agent can read, write, edit, search and manage files quickly and safely, without wasting its context or guessing what happened.
It serves agents first, and the people who run them second.
It turns the file operations an agent performs constantly into one small binary with token-dense, truthful output.
It owns exactly one thing: working with files on the local filesystem.

## Nothing reports success when it did not succeed

This rule sits above every other rule here.
A command that skipped, overwrote or refused part of its work says so, with what and why.
An edit that matched nothing, an archive entry that was skipped, and a missing input are reported, never folded into a plain success.
An empty result and a failed command are never reported the same way.
A test asserts the effect on disk, not the exit code.

## Safe by default

Writes stay inside the paths the caller names; an archive entry that escapes its target directory is refused and reported.
Existing files are not overwritten silently; an overwrite is either refused or reported.
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
Semantic search belongs to `llm-semantic-axi`, deterministic project facts to `llm-data-axi`, and web or model work to `llm-extract-axi`.
Install and upgrade are one command, verified by checksum, on Linux and macOS; Windows is best effort.
Plans and debt live in a private planning repo.

A change aligns when it makes file work faster, safer, more truthful about what happened, or cheaper in tokens.
A change should be resisted when it hides a partial failure, calls a model or the network, adds a heavy dependency, or moves work here that belongs to a sibling tool.

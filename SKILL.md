---
name: witc
description: Generate summary of a Go project
license: MIT
compatibility: opencode
---

## Main description

You have access to run a shell command that can summarize for you a codebase structure.
It works only with Go projects at the moment.

## How to use

Simply cd to a root of a project you need to summarize and run this command using shell tool:

witc summarize .

The output will contain info about file structure and outline of project's packages, structs and methods.

## When to use

Use this command when you yet have no context about a codebase. It will help you find the places you need to edit or read quicker.

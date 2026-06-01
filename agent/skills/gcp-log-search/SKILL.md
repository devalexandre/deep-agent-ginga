---
name: gcp-log-search
description: "Searches GCP logs for specific terms or conditions."
metadata:
  version: "0.1.0"
  author: devalexandre
---

# Skill: GCP Log Search

## Purpose

Use this skill when the user wants to search, inspect, debug, or investigate logs in Google Cloud Platform using the `gcloud` CLI.

This skill is focused on Cloud Logging queries for:

- Cloud Run
- App Engine
- Cloud Tasks
- Cloud Functions
- GKE
- Compute Engine
- Cloud Audit Logs
- generic GCP logs

## When To Use

Use this skill when the user asks things like:

- "search this error in GCP logs"
- "find logs for this service"
- "check Cloud Run errors"
- "look for this idcompra in logs"
- "find Cloud Tasks delete/pause/resume activity"
- "search for this payload"
- "check logs from the last hour"
- "find errors in production"
- "search by trace, request id, cpf, idrelatorio, idcompra, idpagamento"

## Core Behavior

Always use `ShellTool` to run `gcloud` commands.

Before searching logs:

1. Identify the active GCP project.
2. Identify the target resource when possible.
3. Identify the time range.
4. Identify the search term or error.
5. Prefer targeted queries over broad queries.
6. Prefer JSON output when the result needs analysis.
7. Never run destructive commands.

## Safety Rules

- Never change GCP resources.
- Never delete logs, sinks, buckets, queues, services, revisions, or IAM policies.
- Never run deployment commands.
- Never enable or disable services.
- Only use read-only commands.
- Do not expose secrets, tokens, credentials, Authorization headers, private keys, or full sensitive payloads in the final answer.
- If logs contain CPF, CNPJ, bank account, tokens, or credentials, summarize and mask sensitive values.

## Default Assumptions

If the user does not provide details, use:

- Project: current `gcloud` configured project
- Time range: last 1 hour
- Limit: 50
- Severity: any severity unless the user asks for errors
- Output: `json` for analysis, `table` for quick display

## Useful Inspection Commands

Check active project:

```bash
gcloud config get-value project
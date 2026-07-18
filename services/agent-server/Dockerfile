# Pre-installs the Paca MCP server globally so `npx -y @paca-ai/paca-mcp`
# resolves from the local install instead of downloading the package on
# every ephemeral sandbox container boot.
#
# Pinned to the commit tagged v1.34.0 upstream — NOT `:latest-python`. That
# floating tag tracks upstream's main branch continuously (built on every
# commit), which runs ahead of their tagged PyPI releases: it was already
# serving events with a `parent_id` field before openhands-sdk on PyPI even
# had one, breaking every conversation against services/ai-agent's
# pip-installed client (pydantic rejects the unrecognized field under
# `extra="forbid"`). There is no way to keep a PyPI-installed client in sync
# with a tag that isn't tracking PyPI releases, so this must stay pinned.
# When bumping openhands-sdk/openhands-tools in services/ai-agent's
# pyproject.toml, bump this to the matching `<short-sha>-python` tag for that
# same version's git tag in the same change — never independently.
FROM ghcr.io/openhands/agent-server:2eff609-python

# The base image runs as the unprivileged `openhands` user, which doesn't own
# npm's global install dir — switch to root for the install, then drop back
# so the container still runs unprivileged at runtime.
USER root
RUN npm install -g @paca-ai/paca-mcp
USER openhands

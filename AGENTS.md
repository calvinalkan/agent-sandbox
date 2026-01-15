## Overview

`agent-sandbox` is a CLI tool that runs commands inside a filesystem sandbox using [bwrap](https://github.com/containers/bubblewrap) (bubblewrap). It protects system files and sensitive data from modification while allowing agents to work within designated areas.

**Platform:** Linux only (requires bwrap and Linux namespaces)

There is a cli in `cmd/agent-sandbox` and a lower-level
library in `pkg/agent-sandbox`. The latter is used by the former.

## Essential commands

```bash
make build

make lint

make fmt

make test
```

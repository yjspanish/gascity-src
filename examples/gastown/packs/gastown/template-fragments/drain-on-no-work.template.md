## Drain on no work

First action on every wake: check for work assigned to you.

  gc bd ready --json --rig {{.RigName}} --assignee {{.AgentName}} \
    | jq 'length'

If the count is 0, do NOT explore, do NOT read code, do NOT
introspect. Immediately:

  gc runtime drain-ack
  exit

If the count is > 0, proceed with normal work.

Rationale: idle context burns tokens. Done means gone.

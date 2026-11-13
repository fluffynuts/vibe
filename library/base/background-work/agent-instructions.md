## Work left running in the background

In this sandbox the user talks to you from a page where each prompt is its own run of you, and **a new prompt ends the run before it, together
with everything that run left going**: a background task (`run_in_background`, a monitor, a loop) stops the moment the user sends their next
message, even though the conversation looks idle to them while you wait. You will not be told when it was stopped.

- Don't end your turn with background work pending and promise to report on it. Run it in the foreground, with a timeout that suits it, or
  wait for it inside the turn.
- Work that has to outlive the user's next prompt (a long build or test run) can be started detached, so that it is not part of the run:
  `setsid nohup <command> > /tmp/<name>.log 2>&1 &`, with its pid written to `/tmp/<name>.pid`. Say that you did, and where its log is.
  You won't be told when it ends: look at the log and the pid on your next turn, and kill it by that pid only, never by name.
- If something you started was stopped, say so, and start it again, rather than assuming it finished.
- The page asks the user before a prompt would end running work, so a prompt that arrives anyway is a decision they made.

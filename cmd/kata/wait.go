package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"go.kenn.io/kata/internal/textsafe"
)

// ExitWaitTimeout is the dedicated exit code `kata wait` returns when the
// --timeout deadline elapses before every ref's condition is met. It sits
// just past the shared exit-code block in helpers.go (ExitOK=0 …
// ExitDaemonUnavail=7); 8 is the first unused value. It lives here rather
// than in helpers.go because wait is the only command that produces it and
// the task scope is limited to this file.
const ExitWaitTimeout = 8

// kindTimeout is the errKind carried by the timeout cliError so the --agent
// and --json error surfaces classify a wait timeout as "timeout" rather than
// a generic internal failure.
const kindTimeout errKind = "timeout"

const (
	// defaultWaitPollInterval keeps the human default gentle; tests pass tiny
	// values via --poll-interval.
	defaultWaitPollInterval = 2 * time.Second
	// maxWaitConsecutiveFails bounds transient GET retries per ref before the
	// command gives up (daemon restart / network blip tolerance).
	maxWaitConsecutiveFails = 3
	// attentionKey / attentionMsgKey are the flat (dotted) metadata keys the
	// attention convention writes; opaque to the daemon.
	attentionKey    = "work.attention"
	attentionMsgKey = "work.attention_msg"
	// attentionOK is the "nothing to see here" attention level; --until
	// attention treats any other non-empty value as satisfied.
	attentionOK = "ok"
)

// waitMode is the --until target condition.
type waitMode string

const (
	waitClosed     waitMode = "closed"
	waitAttention  waitMode = "attention"
	waitNeedsHuman waitMode = "needs-human"
	waitStuck      waitMode = "stuck"
)

// issueState is the minimal issue snapshot the wait condition evaluator reads:
// the lifecycle status and the value of the work.attention metadata key ("" when
// unset). Keeping the evaluator input this small makes evalWait a pure function
// so an event-driven fast path can replace the poll loop later without touching
// the semantics.
type issueState struct {
	status    string
	attention string
}

// evalWait is the pure condition-evaluation core: given a wait mode and an
// issue snapshot, it reports whether the wait for that ref is satisfied and the
// reason ("closed" or "attention"). A closed issue satisfies every mode — you
// cannot wait forever on a finished issue — and closed always reports "closed"
// so callers can distinguish it from an attention transition.
func evalWait(mode waitMode, st issueState) (satisfied bool, reason string) {
	if st.status == "closed" {
		return true, "closed"
	}
	switch mode {
	case waitNeedsHuman:
		if st.attention == string(waitNeedsHuman) {
			return true, "attention"
		}
	case waitStuck:
		if st.attention == string(waitStuck) {
			return true, "attention"
		}
	case waitAttention:
		if st.attention != "" && st.attention != attentionOK {
			return true, "attention"
		}
	case waitClosed:
		// Only a closed issue satisfies --until closed; handled above.
	}
	return false, ""
}

// parseWaitMode validates the --until value.
func parseWaitMode(s string) (waitMode, error) {
	switch waitMode(s) {
	case waitClosed, waitAttention, waitNeedsHuman, waitStuck:
		return waitMode(s), nil
	default:
		return "", &cliError{
			Message:  fmt.Sprintf("--until must be one of closed|attention|needs-human|stuck, got %q", s),
			Kind:     kindValidation,
			ExitCode: ExitValidation,
		}
	}
}

// waitResult is one ref's outcome in the --json payload.
type waitResult struct {
	Ref          string `json:"ref"`
	Reason       string `json:"reason"`
	Attention    string `json:"attention,omitempty"`
	AttentionMsg string `json:"attention_msg,omitempty"`
	WaitedMs     int64  `json:"waited_ms"`
}

// waitJSONOutput is the single object emitted under --json at the end of a wait.
type waitJSONOutput struct {
	Results  []waitResult `json:"results"`
	TimedOut bool         `json:"timed_out"`
	Pending  []string     `json:"pending"`
}

// waitTarget is a ref's mutable per-run state.
type waitTarget struct {
	arg       string // user-supplied ref, used verbatim for display
	pid       int64
	refForAPI string
	fails     int
	done      bool
	result    waitResult
}

type waitOptions struct {
	until        string
	timeout      time.Duration
	pollInterval time.Duration
	anyMode      bool
	allMode      bool
}

func newWaitCmd() *cobra.Command {
	var opts waitOptions
	cmd := &cobra.Command{
		Use:   "wait <ref> [<ref>...]",
		Short: "block until issues close or need attention",
		Long: `Block until one or more issues reach a target condition.

wait is a read-only fan-out/join primitive: it polls each ref's state and
returns when the --until condition is met. --until closed (default) waits for
the issue to be closed; --until needs-human / --until stuck wait for the
work.attention metadata key to reach that value; --until attention fires on any
attention level other than "ok". A closed issue also completes the wait in the
attention modes (you cannot wait forever on a finished issue); the reason then
reads "closed" rather than "attention".

With multiple refs, --all (default) waits for every ref and --any returns as
soon as the first fires. On --timeout expiry the still-pending refs are reported
and the command exits with a dedicated exit code (` + fmt.Sprint(ExitWaitTimeout) + `).

The --json output is a single object:

  {
    "results": [
      {"ref": "abc4", "reason": "closed", "waited_ms": 152},
      {"ref": "de5f", "reason": "attention", "attention": "needs-human",
       "attention_msg": "blocked on migration", "waited_ms": 3004}
    ],
    "timed_out": false,
    "pending": []
  }

reason is "closed" or "attention"; attention/attention_msg are present only for
attention completions; pending lists refs still unmet on timeout.`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWait(cmd, args, opts)
		},
	}
	cmd.Flags().StringVar(&opts.until, "until", "closed",
		"condition to wait for: closed|attention|needs-human|stuck")
	cmd.Flags().DurationVar(&opts.timeout, "timeout", 0,
		"maximum time to wait (0 = wait forever)")
	cmd.Flags().DurationVar(&opts.pollInterval, "poll-interval", defaultWaitPollInterval,
		"state poll cadence (must be > 0)")
	cmd.Flags().BoolVar(&opts.anyMode, "any", false,
		"return as soon as the first ref's condition is met")
	cmd.Flags().BoolVar(&opts.allMode, "all", false,
		"wait for every ref's condition to be met (default)")
	return cmd
}

func runWait(cmd *cobra.Command, args []string, opts waitOptions) error {
	mode, err := parseWaitMode(opts.until)
	if err != nil {
		return err
	}
	if opts.anyMode && opts.allMode {
		return &cliError{
			Message:  "--any and --all are mutually exclusive",
			Kind:     kindUsage,
			ExitCode: ExitUsage,
		}
	}
	if opts.pollInterval <= 0 {
		return &cliError{
			Message:  "--poll-interval must be greater than 0",
			Kind:     kindValidation,
			ExitCode: ExitValidation,
		}
	}
	anyMode := opts.anyMode // --all is the default when neither is set

	ctx := cmd.Context()
	targets := make([]*waitTarget, 0, len(args))
	var baseURL string
	for _, arg := range args {
		c, resolvedURL, pid, ref, rerr := resolveIssueRefForCommand(cmd, arg)
		if rerr != nil {
			return rerr
		}
		ctx = c
		baseURL = resolvedURL
		targets = append(targets, &waitTarget{arg: arg, pid: pid, refForAPI: ref.RefForAPI})
	}
	client, err := httpClientFor(ctx, baseURL)
	if err != nil {
		return err
	}

	reporter := &waitReporter{mode: currentOutputMode(), w: cmd.OutOrStdout()}
	start := time.Now()

	// Initial pass: fail fast on any unresolvable ref (listing which), and
	// complete refs whose condition already holds at start.
	var firstFetchErr error
	var badRefs []string
	for _, t := range targets {
		st, msg, ferr := waitFetchState(ctx, client, baseURL, t)
		if ferr != nil {
			if firstFetchErr == nil {
				firstFetchErr = ferr
			}
			badRefs = append(badRefs, t.arg)
			continue
		}
		if evalTarget(mode, t, st, msg, start) {
			if rerr := reporter.report(t); rerr != nil {
				return rerr
			}
		}
	}
	// In --any mode, a ref that already satisfied the join during this same
	// pass means the wait has fired: report success and ignore any bad refs
	// rather than failing on them. --all is unaffected (any bad ref still
	// fails fast); in --any, bad refs still fail fast when the join is not
	// yet satisfied.
	if len(badRefs) > 0 && !(anyMode && waitComplete(targets, anyMode)) {
		if len(badRefs) == 1 {
			return firstFetchErr // preserve the daemon-derived exit code (e.g. 404)
		}
		return &cliError{
			Message:  "cannot resolve refs: " + strings.Join(badRefs, ", "),
			Kind:     kindNotFound,
			ExitCode: ExitNotFound,
		}
	}

	if err := waitPollLoop(ctx, client, baseURL, mode, opts, anyMode, start, targets, reporter); err != nil {
		return err
	}

	timedOut := !waitComplete(targets, anyMode)

	if reporter.mode == outputJSON {
		out := waitJSONOutput{
			Results:  collectResults(targets),
			TimedOut: timedOut,
			Pending:  pendingRefs(targets),
		}
		var buf bytes.Buffer
		if err := emitJSON(&buf, out); err != nil {
			return err
		}
		if _, err := fmt.Fprint(cmd.OutOrStdout(), buf.String()); err != nil {
			return err
		}
	}

	if timedOut {
		return &cliError{
			Message: fmt.Sprintf("wait timed out after %s; pending: %s",
				opts.timeout, strings.Join(pendingRefs(targets), ", ")),
			Kind:     kindTimeout,
			ExitCode: ExitWaitTimeout,
		}
	}
	return nil
}

// waitPollLoop polls the still-pending targets on the configured cadence until
// the join condition is met, the timeout deadline passes, or the context is
// cancelled. It returns an error only for a hard failure (context cancellation
// or a ref that has failed too many consecutive fetches); timeout is signalled
// to the caller via the targets' completion state, not an error here.
func waitPollLoop(
	ctx context.Context,
	client *http.Client,
	baseURL string,
	mode waitMode,
	opts waitOptions,
	anyMode bool,
	start time.Time,
	targets []*waitTarget,
	reporter *waitReporter,
) error {
	var deadline time.Time
	if opts.timeout > 0 {
		deadline = start.Add(opts.timeout)
	}
	for !waitComplete(targets, anyMode) {
		if !deadline.IsZero() && !time.Now().Before(deadline) {
			return nil // timed out; caller detects via waitComplete
		}
		sleep := opts.pollInterval
		if !deadline.IsZero() {
			if rem := time.Until(deadline); rem < sleep {
				sleep = rem
			}
		}
		if sleep > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(sleep):
			}
		}
		for _, t := range targets {
			if t.done {
				continue
			}
			st, msg, err := waitFetchState(ctx, client, baseURL, t)
			if err != nil {
				t.fails++
				if t.fails >= maxWaitConsecutiveFails {
					return &cliError{
						Message: fmt.Sprintf("wait: %s: %d consecutive failures fetching issue state: %v",
							t.arg, t.fails, err),
						Kind:     kindInternal,
						ExitCode: ExitInternal,
					}
				}
				continue
			}
			t.fails = 0
			if evalTarget(mode, t, st, msg, start) {
				if rerr := reporter.report(t); rerr != nil {
					return rerr
				}
				if anyMode {
					return nil
				}
			}
		}
	}
	return nil
}

// evalTarget evaluates st against mode for a not-yet-done target. When the
// condition is newly satisfied it records the result (sanitizing user-visible
// attention text) and returns true.
func evalTarget(mode waitMode, t *waitTarget, st issueState, msg string, start time.Time) bool {
	if t.done {
		return false
	}
	satisfied, reason := evalWait(mode, st)
	if !satisfied {
		return false
	}
	t.done = true
	t.result = waitResult{
		Ref:      t.arg,
		Reason:   reason,
		WaitedMs: time.Since(start).Milliseconds(),
	}
	if reason == "attention" {
		t.result.Attention = textsafe.Line(st.attention)
		t.result.AttentionMsg = textsafe.Line(msg)
	}
	return true
}

// waitFetchState GETs one issue and reduces it to the evaluator's inputs plus
// the attention message. A 4xx/5xx status becomes a *cliError (so a bad ref
// keeps the daemon's exit code); transport errors propagate for retry.
func waitFetchState(ctx context.Context, client *http.Client, baseURL string, t *waitTarget) (issueState, string, error) {
	getURL := fmt.Sprintf("%s/api/v1/projects/%d/issues/%s", baseURL, t.pid, url.PathEscape(t.refForAPI))
	status, bs, err := httpDoJSON(ctx, client, http.MethodGet, getURL, nil)
	if err != nil {
		return issueState{}, "", err
	}
	if status >= 400 {
		return issueState{}, "", apiErrFromBody(status, bs)
	}
	var out struct {
		Issue struct {
			Status   string                     `json:"status"`
			Metadata map[string]json.RawMessage `json:"metadata"`
		} `json:"issue"`
	}
	if err := json.Unmarshal(bs, &out); err != nil {
		return issueState{}, "", err
	}
	st := issueState{
		status:    out.Issue.Status,
		attention: decodeJSONString(out.Issue.Metadata[attentionKey]),
	}
	return st, decodeJSONString(out.Issue.Metadata[attentionMsgKey]), nil
}

// decodeJSONString unwraps a JSON string metadata value, returning "" for
// absent, null, or non-string values (attention values are opaque strings).
func decodeJSONString(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return ""
	}
	return s
}

// waitComplete reports whether the join condition is met: every target for
// --all, at least one for --any.
func waitComplete(targets []*waitTarget, anyMode bool) bool {
	done := 0
	for _, t := range targets {
		if t.done {
			done++
		}
	}
	if anyMode {
		return done > 0
	}
	return done == len(targets)
}

func collectResults(targets []*waitTarget) []waitResult {
	out := make([]waitResult, 0, len(targets))
	for _, t := range targets {
		if t.done {
			out = append(out, t.result)
		}
	}
	return out
}

func pendingRefs(targets []*waitTarget) []string {
	out := make([]string, 0)
	for _, t := range targets {
		if !t.done {
			out = append(out, t.arg)
		}
	}
	return out
}

// waitReporter streams per-ref completion lines for the human and agent output
// modes. The JSON mode collects a single object at the end instead, so report
// is a no-op there.
type waitReporter struct {
	mode outputMode
	w    io.Writer
}

func (r *waitReporter) report(t *waitTarget) error {
	switch r.mode {
	case outputJSON:
		return nil
	case outputAgent:
		return r.reportAgent(t)
	default:
		return r.reportHuman(t)
	}
}

func (r *waitReporter) reportHuman(t *waitTarget) error {
	ref := textsafe.Line(t.arg)
	if t.result.Reason == "closed" {
		_, err := fmt.Fprintf(r.w, "%s closed\n", ref)
		return err
	}
	line := fmt.Sprintf("%s attention: %s", ref, t.result.Attention)
	if t.result.AttentionMsg != "" {
		line += " — " + t.result.AttentionMsg
	}
	_, err := fmt.Fprintln(r.w, line)
	return err
}

func (r *waitReporter) reportAgent(t *waitTarget) error {
	var b strings.Builder
	fmt.Fprintf(&b, "OK wait %s reason=%s", agentValue(t.arg), t.result.Reason)
	if t.result.Attention != "" {
		fmt.Fprintf(&b, " attention=%s", agentValue(t.result.Attention))
	}
	if t.result.AttentionMsg != "" {
		fmt.Fprintf(&b, " message=%s", agentValue(t.result.AttentionMsg))
	}
	_, err := fmt.Fprintln(r.w, b.String())
	return err
}

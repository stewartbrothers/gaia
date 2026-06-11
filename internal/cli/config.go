package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/stewartbrothers/gaia/core/doctor"
	"github.com/stewartbrothers/gaia/core/exitcode"
)

// newConfigCmd registers `gaia config …` subcommands. v1 carries
// only `doctor` (#277); future home for `gaia config show`,
// `gaia config edit`, etc. The umbrella exists so the CLI surface
// has a stable place to hang configuration-related operations.
func newConfigCmd(flags *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Inspect and lint gaia's configuration",
		Long: `Commands that introspect gaia's config + credentials surface
without mutating it. Use these to debug "why is gaia doing X?" or
to gate CI on a clean configuration.`,
	}
	cmd.AddCommand(newConfigDoctorCmd(flags))
	return cmd
}

// newConfigDoctorCmd implements `gaia config doctor` (#277).
//
// Walks the resolved config + credential layers and prints findings
// (OK / INFO / WARN / ERR) covering multi-project safety, credential
// hygiene, profile coherence, repo resolution, and cwd context.
//
// Exit codes:
//   - 0 — no ERR findings (WARN allowed unless --strict).
//   - 1 — ≥1 ERR finding (or any WARN when --strict).
//
// Auto-fix is explicitly out of scope. Cross-project scanning is
// out of scope. MCP exposure lands in a follow-up once the CLI
// shape stabilizes.
func newConfigDoctorCmd(flags *globalFlags) *cobra.Command {
	var (
		strict bool
		quiet  bool
	)
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Lint gaia's resolved config + credentials and report actionable findings",
		Long: `Reports findings against the resolved config + credentials in the
cwd. Each finding has a level (OK / INFO / WARN / ERR), a stable
code, a one-line message, and a remediation hint.

Default invocation prints one line per finding, sorted by check
group (not severity — diff-friendly). --format json emits the
standard envelope with findings as data records.

Exit code: 0 when no ERR. 1 when any ERR. --strict promotes WARN
to ERR for CI gating.

Checks performed:

  - Multi-project safety: warn if the global config sets
    default_profile or default_repo (these belong per-project).

  - Credential hygiene: error if the global credentials file is
    not 0600; error if a project credentials file is not
    gitignored; warn if a token env var overlaps a stored
    credential for the same provider.

  - Profile coherence: error if the resolved profile has no
    provider or no api_url; warn if token_env is empty without a
    canonical fallback; warn if default_profile names a missing
    profile.

  - Workflows precedence: warn if the checkout has both
    .forgejo/workflows and .github/workflows AND the .forgejo set
    does not cover every .github workflow (Forgejo ignores
    .github/workflows when .forgejo/workflows exists — silently
    disabling the shadowed .github CI gate).

  - Repo resolution: info showing how owner/name would resolve
    in the cwd.

  - Cwd context: info listing which config layers contributed.

Out of scope (v1): auto-fix, cross-project scanning, MCP exposure.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			explicitFormat := cmd.Flags().Changed("format")
			format := flags.Format

			in, err := buildDoctorInputs(flags)
			if err != nil {
				return err
			}
			findings := doctor.Run(in)
			if strict {
				findings = doctor.PromoteWarnings(findings)
			}

			// --quiet: exit code only, no output (CI gating).
			if quiet {
				if doctor.HasErrors(findings) {
					return exitcode.Errorf(exitcode.Generic,
						"%d ERR finding(s) (rerun without --quiet to list them)", countErrs(findings))
				}
				return nil
			}

			// --format json / pretty go through the envelope; raw
			// text is the default for direct human consumption.
			if explicitFormat && (format == "json" || format == "pretty") {
				if err := renderEnvelope(cmd, flags, findings, nil, prettyDoctor); err != nil {
					return err
				}
				if doctor.HasErrors(findings) {
					return exitcode.Errorf(exitcode.Generic,
						"%d ERR finding(s)", countErrs(findings))
				}
				return nil
			}

			// Raw text path: one finding per line.
			writeDoctorText(cmd.OutOrStdout(), findings)
			if doctor.HasErrors(findings) {
				return exitcode.Errorf(exitcode.Generic,
					"%d ERR finding(s)", countErrs(findings))
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&strict, "strict", false, "promote WARN findings to ERR for CI gating")
	cmd.Flags().BoolVar(&quiet, "quiet", false, "suppress output (use for exit-code-only gating)")
	return cmd
}

// buildDoctorInputs assembles doctor.Inputs from the resolved
// Settings handle. Every layer doctor inspects (configs, paths,
// credentials, env-var snapshot, git-remote autodetect) is already
// computed in settings.Load; we just project from the Inspector
// view here. No file I/O happens in this function.
//
// One simplification vs forgebuilder.Build: doctor doesn't need
// the resolved provider Token (we report on env-var presence
// alone, never read the token value), so the Settings.Token() field
// is never consulted.
func buildDoctorInputs(flags *globalFlags) (doctor.Inputs, error) {
	s, err := loadSettings(flags)
	if err != nil {
		return doctor.Inputs{}, exitcode.Wrap(err, exitcode.Generic, "load settings")
	}
	i := s.Inspector()
	return doctor.Inputs{
		Profile:                i.ProfileFlag(),
		RepoFlag:               i.RepoFlag(),
		GlobalConfigPath:       i.GlobalConfigPath(),
		Global:                 i.GlobalConfig(),
		ProjectConfigPath:      i.ProjectConfigPath(),
		Project:                i.ProjectConfig(),
		Cwd:                    i.Cwd(),
		RepoRoot:               i.RepoRoot(),
		GlobalCredentialsPath:  i.GlobalCredentialsPath(),
		ProjectCredentialsPath: i.ProjectCredentialsPath(),
		Credentials:            i.Credentials(),
		EnvVars:                i.EnvVars(),
		GitRemoteRepo:          i.GitRemoteRepo(),
	}, nil
}

// countErrs is a tiny helper kept here so the CLI message is
// stable: doctor.HasErrors returns bool; the count is just for
// the message.
func countErrs(findings []doctor.Finding) int {
	n := 0
	for _, f := range findings {
		if f.Level == doctor.LevelErr {
			n++
		}
	}
	return n
}

// writeDoctorText renders findings as one line per finding in the
// shape "<level> <code>: <message>" with an indented remediation
// (if any) on the next line. Designed for direct human reading;
// CI scripts that want structure use --format json.
func writeDoctorText(w io.Writer, findings []doctor.Finding) {
	if len(findings) == 0 {
		_, _ = fmt.Fprintln(w, "no findings — gaia config looks clean.")
		return
	}
	for _, f := range findings {
		_, _ = fmt.Fprintf(w, "%s %s: %s\n", f.Level, f.Code, f.Message)
		if f.Remediation != "" {
			_, _ = fmt.Fprintf(w, "    fix: %s\n", f.Remediation)
		}
		if f.SourceFile != "" {
			_, _ = fmt.Fprintf(w, "    source: %s\n", f.SourceFile)
		}
	}
	// Summary line: one-shot exit-code preview so an operator
	// scanning the output knows whether the run "failed".
	errs := countErrs(findings)
	warns := 0
	for _, f := range findings {
		if f.Level == doctor.LevelWarn {
			warns++
		}
	}
	_, _ = fmt.Fprintf(w, "\nsummary: %d ERR, %d WARN, %d total finding(s)\n", errs, warns, len(findings))
}

// prettyDoctor is the --format pretty renderer. Reuses
// writeDoctorText for shape parity with the raw-text path; the
// difference is purely envelope wrapping vs direct emission.
func prettyDoctor(w io.Writer, data any) error {
	findings, ok := data.([]doctor.Finding)
	if !ok {
		// Some envelope projection paths reshape to []any. Be
		// permissive: render whatever we got via type-asserted
		// reflection-light path. The common path is the typed
		// slice from doctor.Run.
		writeDoctorTextAny(w, data)
		return nil
	}
	writeDoctorText(w, findings)
	return nil
}

// writeDoctorTextAny is the fallback for when --fields-style
// projection rewraps the typed slice into []any. Doctor doesn't
// expose --fields meaningfully (every finding is the same shape)
// so this is more defensive than load-bearing.
func writeDoctorTextAny(w io.Writer, data any) {
	xs, ok := data.([]any)
	if !ok {
		_, _ = fmt.Fprintf(w, "%v\n", data)
		return
	}
	for _, x := range xs {
		if m, isMap := x.(map[string]any); isMap {
			level := fmt.Sprint(m["level"])
			code := fmt.Sprint(m["code"])
			msg := fmt.Sprint(m["message"])
			_, _ = fmt.Fprintf(w, "%s %s: %s\n", strings.ToUpper(level), code, msg)
			continue
		}
		_, _ = fmt.Fprintf(w, "%v\n", x)
	}
}

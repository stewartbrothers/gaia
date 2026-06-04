package cli

import (
	"fmt"
	"io"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/stewartbrothers/gaia/core/exitcode"
	"github.com/stewartbrothers/gaia/core/provider"
	"github.com/stewartbrothers/gaia/core/types"
)

func newWebhookCmd(flags *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "webhook",
		Short: "List, create, edit, delete, and test repo webhooks; inspect deliveries",
		Long: `Manage repo webhooks: CRUD + delivery history + redeliver + test.

Closes the only "must use curl" gap from the project dogfood
contract — gaia now covers webhook config end-to-end on both
Forgejo and GitHub.`,
	}
	cmd.AddCommand(newWebhookListCmd(flags))
	cmd.AddCommand(newWebhookViewCmd(flags))
	cmd.AddCommand(newWebhookCreateCmd(flags))
	cmd.AddCommand(newWebhookEditCmd(flags))
	cmd.AddCommand(newWebhookDeleteCmd(flags))
	cmd.AddCommand(newWebhookDeliveriesCmd(flags))
	cmd.AddCommand(newWebhookRedeliverCmd(flags))
	cmd.AddCommand(newWebhookTestCmd(flags))
	return cmd
}

func newWebhookListCmd(flags *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List webhooks configured on the repo",
		RunE: func(cmd *cobra.Command, _ []string) error {
			p, err := buildWebhookOps(flags)
			if err != nil {
				return err
			}
			owner, repo, err := resolveRepo(flags)
			if err != nil {
				return err
			}
			if flags.Format == "ndjson" {
				return renderListStreaming(cmd, flags, func(cursor string) ([]any, *provider.Page, error) {
					hooks, page, err := p.ListWebhooks(cmd.Context(), owner, repo, provider.ListWebhooksOptions{
						Limit:  flags.Limit,
						Cursor: cursor,
					})
					if err != nil {
						return nil, nil, err
					}
					return toAnySlice(hooks), page, nil
				})
			}
			hooks, page, err := p.ListWebhooks(cmd.Context(), owner, repo, provider.ListWebhooksOptions{
				Limit:  flags.Limit,
				Cursor: flags.Cursor,
			})
			if err != nil {
				return err
			}
			return renderEnvelope(cmd, flags, hooks, page, prettyWebhookList)
		},
	}
}

func newWebhookViewCmd(flags *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "view <id>",
		Short: "View one webhook by ID",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := parseHookID(args[0])
			if err != nil {
				return err
			}
			p, err := buildWebhookOps(flags)
			if err != nil {
				return err
			}
			owner, repo, err := resolveRepo(flags)
			if err != nil {
				return err
			}
			h, err := p.GetWebhook(cmd.Context(), owner, repo, id)
			if err != nil {
				return err
			}
			return renderEnvelope(cmd, flags, h, nil, prettyWebhookView)
		},
	}
}

func newWebhookCreateCmd(flags *globalFlags) *cobra.Command {
	var (
		urlFlag     string
		contentType string
		secret      string
		events      []string
		active      bool
		dryRun      bool
	)
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new webhook",
		Long: `Install a new webhook on the repo.

  $ gaia webhook create \
      --url https://example.com/hook \
      --content-type json \
      --secret "$GAIA_HOOK_SECRET" \
      --events push,pull_request \
      --active

The secret travels in the request body but never lands in
gaia's response. Both Forgejo and GitHub redact secret on read,
and the trimmed Webhook type deliberately has no Secret field
so downstream rendering can't leak it.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if urlFlag == "" {
				return exitcode.Errorf(exitcode.Usage, "--url is required")
			}
			if contentType != "json" && contentType != "form" {
				return exitcode.Errorf(exitcode.Usage, "--content-type must be json or form; got %q", contentType)
			}
			if len(events) == 0 {
				return exitcode.Errorf(exitcode.Usage, "--events is required (at least one event)")
			}
			opts := provider.CreateWebhookOptions{
				URL:         urlFlag,
				ContentType: contentType,
				Secret:      secret,
				Events:      events,
				Active:      active,
			}
			p, err := buildWebhookOps(flags)
			if err != nil {
				return err
			}
			owner, repo, err := resolveRepo(flags)
			if err != nil {
				return err
			}
			if dryRun {
				// Strip the secret from the dry-run output so it doesn't
				// land in shell history or terminal scrollback.
				redacted := opts
				if redacted.Secret != "" {
					redacted.Secret = "<redacted>"
				}
				return printDryRun(cmd, fmt.Sprintf("POST /repos/%s/%s/hooks", owner, repo), redacted)
			}
			h, err := p.CreateWebhook(cmd.Context(), owner, repo, opts)
			if err != nil {
				return err
			}
			return renderEnvelope(cmd, flags, h, nil, prettyWebhookView)
		},
	}
	cmd.Flags().StringVar(&urlFlag, "url", "", "destination URL (required)")
	cmd.Flags().StringVar(&contentType, "content-type", "json", "json or form (default json)")
	cmd.Flags().StringVar(&secret, "secret", "", "HMAC signing secret (sent only on create/edit; never echoed)")
	cmd.Flags().StringSliceVar(&events, "events", nil, "events to subscribe to (comma-separated, e.g. push,pull_request)")
	cmd.Flags().BoolVar(&active, "active", true, "whether the webhook is active immediately")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print the request body (with secret redacted) and exit")
	return cmd
}

func newWebhookEditCmd(flags *globalFlags) *cobra.Command {
	var (
		urlFlag      string
		contentType  string
		secret       string
		addEvents    []string
		removeEvents []string
		activeFlag   bool
		inactiveFlag bool
		dryRun       bool
	)
	cmd := &cobra.Command{
		Use:   "edit <id>",
		Short: "Edit a webhook by ID",
		Long: `Patch a webhook. Empty fields are unchanged.

  $ gaia webhook edit 7 --url https://new.example.com
  $ gaia webhook edit 7 --add-events release --remove-events issues
  $ gaia webhook edit 7 --inactive            # disable
  $ gaia webhook edit 7 --secret "$NEW"       # rotate secret

--active and --inactive are mutually exclusive; pass at most one.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if activeFlag && inactiveFlag {
				return exitcode.Errorf(exitcode.Usage, "--active and --inactive are mutually exclusive")
			}
			id, err := parseHookID(args[0])
			if err != nil {
				return err
			}
			if contentType != "" && contentType != "json" && contentType != "form" {
				return exitcode.Errorf(exitcode.Usage, "--content-type must be json or form; got %q", contentType)
			}
			opts := provider.EditWebhookOptions{
				URL:          urlFlag,
				ContentType:  contentType,
				Secret:       secret,
				AddEvents:    addEvents,
				RemoveEvents: removeEvents,
			}
			if activeFlag {
				v := true
				opts.Active = &v
			} else if inactiveFlag {
				v := false
				opts.Active = &v
			}

			p, err := buildWebhookOps(flags)
			if err != nil {
				return err
			}
			owner, repo, err := resolveRepo(flags)
			if err != nil {
				return err
			}
			if dryRun {
				preview := map[string]any{}
				if opts.URL != "" {
					preview["url"] = opts.URL
				}
				if opts.ContentType != "" {
					preview["content_type"] = opts.ContentType
				}
				if opts.Secret != "" {
					preview["secret"] = "<redacted>"
				}
				if len(opts.AddEvents) > 0 {
					preview["add_events"] = opts.AddEvents
				}
				if len(opts.RemoveEvents) > 0 {
					preview["remove_events"] = opts.RemoveEvents
				}
				if opts.Active != nil {
					preview["active"] = *opts.Active
				}
				return printDryRun(cmd, fmt.Sprintf("PATCH /repos/%s/%s/hooks/%d", owner, repo, id), preview)
			}
			h, err := p.EditWebhook(cmd.Context(), owner, repo, id, opts)
			if err != nil {
				return err
			}
			return renderEnvelope(cmd, flags, h, nil, prettyWebhookView)
		},
	}
	cmd.Flags().StringVar(&urlFlag, "url", "", "new destination URL")
	cmd.Flags().StringVar(&contentType, "content-type", "", "new content type: json or form")
	cmd.Flags().StringVar(&secret, "secret", "", "new HMAC signing secret (rotates the value)")
	cmd.Flags().StringSliceVar(&addEvents, "add-events", nil, "events to add (comma-separated)")
	cmd.Flags().StringSliceVar(&removeEvents, "remove-events", nil, "events to remove (comma-separated)")
	cmd.Flags().BoolVar(&activeFlag, "active", false, "mark webhook active")
	cmd.Flags().BoolVar(&inactiveFlag, "inactive", false, "mark webhook inactive")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print the patch and exit (secret redacted)")
	return cmd
}

func newWebhookDeleteCmd(flags *globalFlags) *cobra.Command {
	var confirm bool
	cmd := &cobra.Command{
		Use:   "delete <id>",
		Short: "Delete a webhook by ID",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := parseHookID(args[0])
			if err != nil {
				return err
			}
			if !confirm {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(),
					"Would delete webhook %d. Re-run with --confirm to actually remove.\n", id)
				return nil
			}
			p, err := buildWebhookOps(flags)
			if err != nil {
				return err
			}
			owner, repo, err := resolveRepo(flags)
			if err != nil {
				return err
			}
			if err := p.DeleteWebhook(cmd.Context(), owner, repo, id); err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "✓ Deleted webhook %d\n", id)
			return nil
		},
	}
	cmd.Flags().BoolVar(&confirm, "confirm", false, "actually delete (without this, prints what would happen)")
	return cmd
}

func newWebhookDeliveriesCmd(flags *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "deliveries <id>",
		Short: "List recent deliveries for a webhook (summaries only — no payload bodies)",
		Long: `Lists recent webhook delivery attempts. Bodies are NOT included
to keep the response token-budget-sensible; a single delivery for
a busy repo can be 50–200 KB. Drill into one delivery for the full
request/response payload via:

  gaia webhook deliveries <id>            # list summaries
  gaia webhook deliveries <id> --get N    # full payload for delivery N`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := parseHookID(args[0])
			if err != nil {
				return err
			}
			p, err := buildWebhookOps(flags)
			if err != nil {
				return err
			}
			owner, repo, err := resolveRepo(flags)
			if err != nil {
				return err
			}
			getID, _ := cmd.Flags().GetInt64("get")
			if getID > 0 {
				detail, err := p.GetWebhookDelivery(cmd.Context(), owner, repo, id, getID)
				if err != nil {
					return err
				}
				return renderEnvelope(cmd, flags, detail, nil, prettyDeliveryDetail)
			}
			if flags.Format == "ndjson" {
				return renderListStreaming(cmd, flags, func(cursor string) ([]any, *provider.Page, error) {
					deliveries, page, err := p.ListWebhookDeliveries(cmd.Context(), owner, repo, id, provider.ListDeliveriesOptions{
						Limit:  flags.Limit,
						Cursor: cursor,
					})
					if err != nil {
						return nil, nil, err
					}
					return toAnySlice(deliveries), page, nil
				})
			}
			deliveries, page, err := p.ListWebhookDeliveries(cmd.Context(), owner, repo, id, provider.ListDeliveriesOptions{
				Limit:  flags.Limit,
				Cursor: flags.Cursor,
			})
			if err != nil {
				return err
			}
			return renderEnvelope(cmd, flags, deliveries, page, prettyDeliveryList)
		},
	}
	cmd.Flags().Int64("get", 0, "fetch one delivery by ID with full request/response payload")
	return cmd
}

func newWebhookRedeliverCmd(flags *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "redeliver <id> <delivery-id>",
		Short: "Re-fire a previous delivery (the receiver sees the same payload)",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := parseHookID(args[0])
			if err != nil {
				return err
			}
			deliveryID, err := strconv.ParseInt(args[1], 10, 64)
			if err != nil {
				return exitcode.Errorf(exitcode.Usage, "delivery-id must be a number; got %q", args[1])
			}
			p, err := buildWebhookOps(flags)
			if err != nil {
				return err
			}
			owner, repo, err := resolveRepo(flags)
			if err != nil {
				return err
			}
			if err := p.RedeliverWebhook(cmd.Context(), owner, repo, id, deliveryID); err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "✓ Redelivered webhook %d delivery %d\n", id, deliveryID)
			return nil
		},
	}
}

func newWebhookTestCmd(flags *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "test <id>",
		Short: "Send a test ping to the webhook",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := parseHookID(args[0])
			if err != nil {
				return err
			}
			p, err := buildWebhookOps(flags)
			if err != nil {
				return err
			}
			owner, repo, err := resolveRepo(flags)
			if err != nil {
				return err
			}
			if err := p.TestWebhook(cmd.Context(), owner, repo, id); err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "✓ Sent test ping to webhook %d\n", id)
			return nil
		},
	}
}

func parseHookID(s string) (int64, error) {
	id, err := strconv.ParseInt(s, 10, 64)
	if err != nil || id <= 0 {
		return 0, exitcode.Errorf(exitcode.Usage, "webhook id must be a positive integer; got %q", s)
	}
	return id, nil
}

func prettyWebhookList(w io.Writer, data any) error {
	hooks, ok := data.([]types.Webhook)
	if !ok {
		return fmt.Errorf("prettyWebhookList: unexpected type %T", data)
	}
	if len(hooks) == 0 {
		_, _ = fmt.Fprintln(w, "(no webhooks)")
		return nil
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "ID\tACTIVE\tCT\tEVENTS\tURL")
	for _, h := range hooks {
		_, _ = fmt.Fprintf(tw, "%d\t%v\t%s\t%s\t%s\n",
			h.ID, h.Active, h.ContentType, strings.Join(h.Events, ","), truncate(h.URL, 60))
	}
	return tw.Flush()
}

func prettyWebhookView(w io.Writer, data any) error {
	h, ok := data.(*types.Webhook)
	if !ok {
		return fmt.Errorf("prettyWebhookView: unexpected type %T", data)
	}
	_, _ = fmt.Fprintf(w, "Webhook #%d\n", h.ID)
	_, _ = fmt.Fprintf(w, "  URL:          %s\n", h.URL)
	_, _ = fmt.Fprintf(w, "  Content-Type: %s\n", h.ContentType)
	_, _ = fmt.Fprintf(w, "  Active:       %v\n", h.Active)
	_, _ = fmt.Fprintf(w, "  Events:       %s\n", strings.Join(h.Events, ", "))
	_, _ = fmt.Fprintf(w, "  Created:      %s\n", h.CreatedAt.Format("2006-01-02 15:04"))
	if !h.UpdatedAt.IsZero() {
		_, _ = fmt.Fprintf(w, "  Updated:      %s\n", h.UpdatedAt.Format("2006-01-02 15:04"))
	}
	return nil
}

func prettyDeliveryList(w io.Writer, data any) error {
	dels, ok := data.([]types.WebhookDelivery)
	if !ok {
		return fmt.Errorf("prettyDeliveryList: unexpected type %T", data)
	}
	if len(dels) == 0 {
		_, _ = fmt.Fprintln(w, "(no deliveries)")
		return nil
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "ID\tEVENT\tSTATUS\tDURATION\tREDELIVERY\tDELIVERED")
	for _, d := range dels {
		_, _ = fmt.Fprintf(tw, "%d\t%s\t%d\t%dms\t%v\t%s\n",
			d.ID, d.Event, d.StatusCode, d.DurationMs, d.Redelivery,
			d.DeliveredAt.Format("2006-01-02 15:04"))
	}
	return tw.Flush()
}

func prettyDeliveryDetail(w io.Writer, data any) error {
	d, ok := data.(*types.WebhookDeliveryDetail)
	if !ok {
		return fmt.Errorf("prettyDeliveryDetail: unexpected type %T", data)
	}
	_, _ = fmt.Fprintf(w, "Delivery #%d (%s)\n", d.ID, d.Event)
	_, _ = fmt.Fprintf(w, "  Status:    %d\n", d.StatusCode)
	_, _ = fmt.Fprintf(w, "  Duration:  %dms\n", d.DurationMs)
	_, _ = fmt.Fprintf(w, "  Delivered: %s\n", d.DeliveredAt.Format("2006-01-02 15:04"))
	if d.Redelivery {
		_, _ = fmt.Fprintln(w, "  (redelivery)")
	}
	if d.RequestBody != "" {
		_, _ = fmt.Fprintf(w, "\n--- Request body ---\n%s\n", d.RequestBody)
	}
	if d.ResponseBody != "" {
		_, _ = fmt.Fprintf(w, "\n--- Response body ---\n%s\n", d.ResponseBody)
	}
	return nil
}

package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/spf13/cobra"

	"github.com/stewartbrothers/gaia/core/cache"
	"github.com/stewartbrothers/gaia/core/exitcode"
)

// newCacheCmd registers `gaia cache ...` subcommands. Today there's
// only one (`nuke`); future commits land `cache stats` and `cache
// inspect` here.
func newCacheCmd(_ *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cache",
		Short: "Manage gaia's local read cache",
		Long: `gaia caches forge reads in a SQLite file per (provider, host) at
~/.cache/gaia/<provider>/<host>.db. Use these subcommands to inspect
or nuke the cache when entries go stale or after a forge migration
where ETags reset.

See docs/cache.md for the cache layout, TTL defaults, and tenant-
safety notes for the HTTP MCP transport.`,
	}
	cmd.AddCommand(newCacheNukeCmd())
	return cmd
}

// newCacheNukeCmd implements `gaia cache nuke [--provider X] [--host Y]`.
// Removes the on-disk SQLite files. Designed to be safe to run twice
// (idempotent) and silent on a missing cache root.
func newCacheNukeCmd() *cobra.Command {
	var (
		provider string
		host     string
	)
	cmd := &cobra.Command{
		Use:   "nuke",
		Short: "Remove cached forge data from disk",
		Long: `Removes the local SQLite cache files. With no flags every cache file
under ~/.cache/gaia is deleted. --provider scopes deletion to one
provider's tree; --host narrows further to a single (provider, host)
pair.

A missing cache directory is NOT an error.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			root, err := cache.DefaultDir()
			if err != nil {
				return exitcode.Wrap(err, exitcode.Generic, "locate cache dir")
			}

			files, err := collectCacheFiles(root, provider, host)
			if err != nil {
				return err
			}
			if len(files) == 0 {
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), "no cache files to remove (nothing to do)")
				return nil
			}

			out := cmd.OutOrStdout()
			for _, f := range files {
				if err := os.Remove(f); err != nil && !os.IsNotExist(err) {
					return exitcode.Wrap(err, exitcode.Generic, "remove "+f)
				}
				_, _ = fmt.Fprintf(out, "removed %s\n", f)
				// Also nuke any sibling SQLite WAL / SHM files left
				// behind by an open handle. journal_mode=WAL produces
				// `<file>-wal` and `<file>-shm`; removing them keeps
				// the directory tidy and avoids confusing operators.
				for _, suffix := range []string{"-wal", "-shm"} {
					_ = os.Remove(f + suffix)
				}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&provider, "provider", "", "scope deletion to one provider (forgejo|github)")
	cmd.Flags().StringVar(&host, "host", "", "scope deletion to a single host (requires --provider)")
	return cmd
}

// collectCacheFiles enumerates every cache .db under root, optionally
// filtered by provider and host. Missing root → empty slice (the
// "nothing to do" path).
func collectCacheFiles(root, provider, host string) ([]string, error) {
	if _, err := os.Stat(root); os.IsNotExist(err) {
		return nil, nil
	} else if err != nil {
		return nil, exitcode.Wrap(err, exitcode.Generic, "stat cache root")
	}

	var out []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if filepath.Ext(path) != ".db" {
			return nil
		}
		if provider != "" {
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			parts := splitPathParts(rel)
			if len(parts) < 2 {
				return nil
			}
			if parts[0] != provider {
				return nil
			}
			if host != "" {
				wantBase := host + ".db"
				if filepath.Base(path) != wantBase {
					return nil
				}
			}
		}
		out = append(out, path)
		return nil
	})
	if err != nil {
		return nil, exitcode.Wrap(err, exitcode.Generic, "walk cache root")
	}
	sort.Strings(out)
	return out, nil
}

// splitPathParts is filepath.SplitList on a single path: ["forgejo",
// "host.db"] for "forgejo/host.db". Doesn't pull in path/filepath.Ext
// gymnastics; one-shot loop.
func splitPathParts(p string) []string {
	var parts []string
	for {
		dir, file := filepath.Split(p)
		if file == "" {
			break
		}
		parts = append([]string{file}, parts...)
		if dir == "" {
			break
		}
		p = filepath.Clean(dir)
		if p == "." || p == string(filepath.Separator) {
			break
		}
	}
	return parts
}

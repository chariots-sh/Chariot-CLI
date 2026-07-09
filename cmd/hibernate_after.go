package cmd

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

var hibernateAfterCmd = &cobra.Command{
	Use:   "hibernate-after",
	Short: "Show how long idle agents run before hibernating",
	Long: `Show how long your agents may sit idle (no inbound message) before Chariot
hibernates them (scales the pod to 0 — session state is kept, and the next
message wakes the agent).

While active, an agent accrues the daily active fee; hibernated it only pays
the small storage fee. A shorter window saves money on quiet fleets; a longer
one avoids cold-start latency on the first message after a lull.

Change it with ` + "`chariot hibernate-after set dd:hh:mm`" + `.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		client, _, err := authedClient()
		if err != nil {
			return err
		}
		a, err := client.Account(cmd.Context())
		if err != nil {
			return err
		}
		out := cmd.OutOrStdout()
		fmt.Fprintf(out, "hibernate after : %s idle (dd:hh:mm)\n", formatDDHHMM(a.HibernateAfterSeconds))
		fmt.Fprintln(out, "\nChange it with `chariot hibernate-after set dd:hh:mm` (or `set default`).")
		return nil
	},
}

var hibernateAfterSetCmd = &cobra.Command{
	Use:   "set <dd:hh:mm|default>",
	Short: "Choose the idle window before agents hibernate",
	Long: `Choose how long your agents may sit idle before they hibernate.

Durations are dd:hh:mm (days:hours:minutes) — hh:mm and plain minutes also
work. Minimum 10 minutes, maximum 90 days. Examples:

  chariot hibernate-after set 02:00:00   # 2 days (the 48h default)
  chariot hibernate-after set 00:01:00   # 1 hour
  chariot hibernate-after set 45         # 45 minutes
  chariot hibernate-after set default    # back to the server default

The change applies from the next hibernation sweep (runs every ~15 minutes),
including to agents that are already idle past the new window.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		var seconds int64
		if args[0] != "default" { // "default" → 0 → reset to the server default
			var err error
			seconds, err = parseDDHHMM(args[0])
			if err != nil {
				return err
			}
		}
		client, _, err := authedClient()
		if err != nil {
			return err
		}
		effective, err := client.SetHibernateAfter(cmd.Context(), seconds)
		if err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "✓ agents hibernate after %s idle (dd:hh:mm)\n", formatDDHHMM(effective))
		return nil
	},
}

// parseDDHHMM parses a duration written as dd:hh:mm, hh:mm, or plain minutes
// into seconds. Parts may exceed their conventional range (e.g. "00:48:00" is
// 48 hours), so anything a human writes with colons means what it says.
func parseDDHHMM(s string) (int64, error) {
	parts := strings.Split(s, ":")
	if len(parts) > 3 {
		return 0, fmt.Errorf("invalid duration %q — use dd:hh:mm (e.g. 02:00:00 for 2 days)", s)
	}
	// Right-align the parts: the last is always minutes, then hours, then days.
	var values [3]int64 // days, hours, minutes
	for i, part := range parts {
		n, err := strconv.ParseInt(strings.TrimSpace(part), 10, 64)
		if err != nil || n < 0 {
			return 0, fmt.Errorf("invalid duration %q — use dd:hh:mm (e.g. 02:00:00 for 2 days)", s)
		}
		values[3-len(parts)+i] = n
	}
	return values[0]*86400 + values[1]*3600 + values[2]*60, nil
}

// formatDDHHMM renders seconds as dd:hh:mm (seconds beyond whole minutes are
// dropped; the backend only stores minute-grained values the CLI produced).
func formatDDHHMM(seconds int64) string {
	if seconds < 0 {
		seconds = 0
	}
	days := seconds / 86400
	hours := (seconds % 86400) / 3600
	minutes := (seconds % 3600) / 60
	return fmt.Sprintf("%02d:%02d:%02d", days, hours, minutes)
}

func init() {
	hibernateAfterCmd.AddCommand(hibernateAfterSetCmd)
	rootCmd.AddCommand(hibernateAfterCmd)
}

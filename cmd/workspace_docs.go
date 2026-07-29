package cmd

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

var (
	workspaceDocsFile      string
	workspaceDocsDeleteYes bool
)

var workspaceDocsCmd = &cobra.Command{
	Use:     "docs <workspace>",
	Aliases: []string{"documents"},
	Short:   "Shared documents the members read and write",
	Long: `List the workspace's shared documents.

Every member can read all of them, add its own, and append to the others' —
this is how the agents leave findings for each other and for you.

  chariot workspace docs research                       # what's there
  chariot workspace docs read research "Q3 findings"    # print one
  chariot workspace docs write research "brief" --file brief.md
  chariot workspace docs delete research "brief"

Documents are addressed by title or id, and are the same ones the web app and
the agents' own docs tool see.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, _, err := authedClient()
		if err != nil {
			return err
		}
		id, err := resolveWorkspace(cmd.Context(), client, args[0])
		if err != nil {
			return err
		}
		docs, err := client.ListWorkspaceDocuments(cmd.Context(), id)
		if err != nil {
			return err
		}
		if len(docs) == 0 {
			fmt.Fprintln(cmd.ErrOrStderr(), "No documents yet — the agents write their own, or add one with `chariot workspace docs write`.")
			return nil
		}
		tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "TITLE\tAUTHOR\tCHARS\tUPDATED")
		for _, d := range docs {
			fmt.Fprintf(tw, "%s\t%s\t%d\t%s\n", d.Title, d.Author, d.ContentChars, d.UpdatedAt.Local().Format("01-02 15:04"))
		}
		return tw.Flush()
	},
}

var workspaceDocsReadCmd = &cobra.Command{
	Use:   "read <workspace> <document>",
	Short: "Print one shared document",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, _, err := authedClient()
		if err != nil {
			return err
		}
		id, err := resolveWorkspace(cmd.Context(), client, args[0])
		if err != nil {
			return err
		}
		doc, err := client.GetWorkspaceDocument(cmd.Context(), id, args[1])
		if err != nil {
			return err
		}
		fmt.Fprintf(cmd.ErrOrStderr(), "# %s — by %s, updated %s\n\n", doc.Title, doc.Author, doc.UpdatedAt.Local().Format("2006-01-02 15:04"))
		fmt.Fprintln(cmd.OutOrStdout(), doc.Content)
		return nil
	},
}

var workspaceDocsWriteCmd = &cobra.Command{
	Use:   "write <workspace> <title>",
	Short: "Create or replace a shared document",
	Long: `Write a shared document the members can read. Content comes from --file, or
from stdin when --file is omitted:

  chariot workspace docs write research "brief" --file brief.md
  echo "focus on margins" | chariot workspace docs write research "brief"

Writing a title that already exists replaces that document's whole body.`,
	Args: cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, _, err := authedClient()
		if err != nil {
			return err
		}
		content, err := documentContent(cmd.InOrStdin())
		if err != nil {
			return err
		}
		id, err := resolveWorkspace(cmd.Context(), client, args[0])
		if err != nil {
			return err
		}
		// A repeat title is an update, not a second document — the backend
		// rejects duplicates, so replace the body of the one that exists.
		existing, err := client.GetWorkspaceDocument(cmd.Context(), id, args[1])
		if err == nil {
			doc, err := client.UpdateWorkspaceDocument(cmd.Context(), id, existing.ID, content)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "✓ updated %q (%d chars)\n", doc.Title, doc.ContentChars)
			return nil
		}
		doc, err := client.CreateWorkspaceDocument(cmd.Context(), id, args[1], content)
		if err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "✓ wrote %q (%d chars)\n", doc.Title, doc.ContentChars)
		return nil
	},
}

var workspaceDocsDeleteCmd = &cobra.Command{
	Use:   "delete <workspace> <document>",
	Short: "Delete a shared document",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, _, err := authedClient()
		if err != nil {
			return err
		}
		id, err := resolveWorkspace(cmd.Context(), client, args[0])
		if err != nil {
			return err
		}
		if !workspaceDocsDeleteYes {
			fmt.Fprintf(cmd.OutOrStdout(), "This deletes %q for every member of %s. Continue? [y/N] ", args[1], args[0])
			line, _ := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
			if strings.ToLower(strings.TrimSpace(line)) != "y" {
				fmt.Fprintln(cmd.OutOrStdout(), "Aborted.")
				return nil
			}
		}
		if err := client.DeleteWorkspaceDocument(cmd.Context(), id, args[1]); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "✓ deleted %q\n", args[1])
		return nil
	},
}

// documentContent reads the document body from --file, else from stdin.
func documentContent(stdin io.Reader) (string, error) {
	if workspaceDocsFile != "" {
		data, err := os.ReadFile(workspaceDocsFile)
		if err != nil {
			return "", err
		}
		return string(data), nil
	}
	data, err := io.ReadAll(stdin)
	if err != nil {
		return "", err
	}
	if len(data) == 0 {
		return "", fmt.Errorf("no content — pass --file <path> or pipe the document in on stdin")
	}
	return string(data), nil
}

func init() {
	workspaceDocsWriteCmd.Flags().StringVar(&workspaceDocsFile, "file", "", "read the document body from this file instead of stdin")
	workspaceDocsDeleteCmd.Flags().BoolVarP(&workspaceDocsDeleteYes, "yes", "y", false, "skip the confirmation prompt")
	workspaceDocsCmd.AddCommand(workspaceDocsReadCmd)
	workspaceDocsCmd.AddCommand(workspaceDocsWriteCmd)
	workspaceDocsCmd.AddCommand(workspaceDocsDeleteCmd)
	workspaceCmd.AddCommand(workspaceDocsCmd)
}

package cmd

import (
	"fmt"
	"strings"

	"github.com/Immortal-Protocols/Chariot-CLI/internal/scaffold"
	"github.com/spf13/cobra"
)

var imageInitDir string

var imageInitCmd = &cobra.Command{
	Use:   "init <template>",
	Short: "Scaffold a ready-to-build custom agent image (e.g. openclaw)",
	Long: `Scaffold a ready-to-build custom agent image.

Writes a build context (Dockerfile + the glue satisfying the Chariot agent
contract) into a new directory. Templates:

  openclaw   An OpenClaw (openclaw.ai) agent: Node gateway, message shim,
             model calls through the Chariot proxy. Needs --pod-size medium.

Then:

  cd chariot-openclaw-image
  docker build -t my-openclaw-agent .
  chariot image push my-openclaw-agent:latest --pod-size medium`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		template := args[0]
		dir := imageInitDir
		if dir == "" {
			dir = fmt.Sprintf("chariot-%s-image", template)
		}
		written, err := scaffold.Write(template, dir)
		if err != nil {
			return err
		}
		out := cmd.OutOrStdout()
		fmt.Fprintf(out, "Scaffolded the %s image template in %s/\n\n", template, dir)
		fmt.Fprintf(out, "  %s\n\n", strings.Join(written, "  "))
		fmt.Fprintln(out, "Next steps:")
		fmt.Fprintf(out, "  cd %s\n", dir)
		fmt.Fprintf(out, "  docker build -t my-%s-agent .\n", template)
		fmt.Fprintf(out, "  chariot image push my-%s-agent:latest --pod-size medium\n\n", template)
		fmt.Fprintf(out, "See %s/README.md for what each file does and how to customize.\n", dir)
		return nil
	},
}

func init() {
	imageInitCmd.Flags().StringVar(&imageInitDir, "dir", "", "target directory (default: ./chariot-<template>-image)")
	imageCmd.AddCommand(imageInitCmd)
}

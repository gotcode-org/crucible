// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (c) 2026 The GotCode Collective
// See the LICENSE file in the project root for full license details.

package ui

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"
)

// Logo contains the official ASCII banner for Crucible.
const Logo = `
   ______               _ _     __     
  / ____/______  ______(_) |__ / /__   
 / /   / ___/ / / / ___/ / __ \/ / _ \ 
/ /___/ /  / /_/ / /__/ / /_/ / /  __/ 
\____/_/   \__,_/\___/_/_.___/_/\___/  
`

// HandleHelp renders standard command help.
func HandleHelp(cmd *cobra.Command, _ []string) {
	if err := cmd.Usage(); err != nil {
		if _, writeErr := fmt.Fprintln(cmd.ErrOrStderr(), ErrorMsg("%v", err)); writeErr != nil {
			return
		}
	}
}

// HandleUsage renders beautifully styled Lip Gloss command usage banners.
func HandleUsage(cmd *cobra.Command) error {
	w := cmd.OutOrStdout()

	if err := printHeader(w, cmd); err != nil {
		return err
	}
	if err := printUsage(w, cmd); err != nil {
		return err
	}
	if err := printCommands(w, cmd); err != nil {
		return err
	}
	if err := printFlags(w, cmd); err != nil {
		return err
	}
	return printFooter(w, cmd)
}

func printHeader(w io.Writer, cmd *cobra.Command) error {
	if _, err := fmt.Fprintln(w, LogoStyle.Render(Logo)); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, HelpTitleStyle.Render(cmd.Short)); err != nil {
		return err
	}
	if cmd.Long != "" {
		if _, err := fmt.Fprintln(w, HelpDescStyle.Render(cmd.Long)); err != nil {
			return err
		}
	}
	return nil
}

func printUsage(w io.Writer, cmd *cobra.Command) error {
	if _, err := fmt.Fprintln(w, HelpSectionStyle.Render("USAGE")); err != nil {
		return err
	}
	if cmd.HasSubCommands() {
		_, err := fmt.Fprintf(w, "  %s [command]\n", cmd.CommandPath())
		return err
	}
	_, err := fmt.Fprintf(w, "  %s\n", cmd.UseLine())
	return err
}

func printCommands(w io.Writer, cmd *cobra.Command) error {
	if len(cmd.Commands()) == 0 {
		return nil
	}
	if _, err := fmt.Fprintln(w, HelpSectionStyle.Render("AVAILABLE COMMANDS")); err != nil {
		return err
	}
	for _, c := range cmd.Commands() {
		if !c.Hidden {
			if _, err := fmt.Fprintf(w, "  %-16s %s\n", CommandStyle.Render(c.Name()), DescriptionStyle.Render(c.Short)); err != nil {
				return err
			}
		}
	}
	return nil
}

func printFlags(w io.Writer, cmd *cobra.Command) error {
	if !cmd.Flags().HasFlags() {
		return nil
	}
	if _, err := fmt.Fprintln(w, HelpSectionStyle.Render("FLAGS")); err != nil {
		return err
	}
	_, err := fmt.Fprintln(w, HelpFlagStyle.Render(cmd.Flags().FlagUsages()))
	return err
}

func printFooter(w io.Writer, cmd *cobra.Command) error {
	if len(cmd.Commands()) == 0 {
		return nil
	}
	if _, err := fmt.Fprintln(w, HelpSectionStyle.Render("LEARN MORE")); err != nil {
		return err
	}
	_, err := fmt.Fprintf(w, "  Use \"%s [command] --help\" for more information about a command.\n", cmd.CommandPath())
	return err
}

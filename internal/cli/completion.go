package cli

import (
	"context"
	"fmt"
	"strings"
)

func completionCommand() *Command {
	return &Command{
		Name:    "completion",
		Summary: "Print a shell completion script",
		Usage:   "vat completion <bash|zsh|fish>",
		Long: `Print a completion script for your shell.

  bash:  vat completion bash > /usr/local/etc/bash_completion.d/vat
  zsh:   vat completion zsh  > "${fpath[1]}/_vat"
  fish:  vat completion fish > ~/.config/fish/completions/vat.fish`,
		Run: func(ctx context.Context, env *Env, args []string) error {
			set := newFlagSet("completion")
			if err := parseFlags(set, args); err != nil {
				return err
			}
			if set.NArg() != 1 {
				return usageErrorf("expected one of: bash, zsh, fish")
			}
			script, err := completionScript(set.Arg(0))
			if err != nil {
				return err
			}
			env.Printer.Println(script)
			return nil
		},
	}
}

func completionScript(shell string) (string, error) {
	root := Root()
	top := make([]string, 0, len(root.Subcommands))
	for _, sub := range root.Subcommands {
		top = append(top, sub.Name)
	}
	topList := strings.Join(top, " ")

	var subCases []string
	for _, sub := range root.Subcommands {
		if len(sub.Subcommands) == 0 {
			continue
		}
		var names []string
		for _, nested := range sub.Subcommands {
			names = append(names, nested.Name)
		}
		subCases = append(subCases, fmt.Sprintf("    %s) echo \"%s\" ;;",
			sub.Name, strings.Join(names, " ")))
	}
	nested := strings.Join(subCases, "\n")

	switch shell {
	case "bash":
		return fmt.Sprintf(`# vat bash completion
_vat_subcommands() {
  case "$1" in
%s
    *) echo "" ;;
  esac
}
_vat() {
  local cur prev
  cur="${COMP_WORDS[COMP_CWORD]}"
  if [ "$COMP_CWORD" -eq 1 ]; then
    COMPREPLY=( $(compgen -W "%s" -- "$cur") )
    return
  fi
  prev="${COMP_WORDS[1]}"
  COMPREPLY=( $(compgen -W "$(_vat_subcommands "$prev")" -- "$cur") )
}
complete -F _vat vat`, nested, topList), nil

	case "zsh":
		return fmt.Sprintf(`#compdef vat
_vat_subcommands() {
  case "$1" in
%s
    *) echo "" ;;
  esac
}
_vat() {
  if (( CURRENT == 2 )); then
    compadd %s
    return
  fi
  compadd ${(z)$(_vat_subcommands ${words[2]})}
}
compdef _vat vat`, nested, topList), nil

	case "fish":
		var lines []string
		lines = append(lines,
			fmt.Sprintf("complete -c vat -n __fish_use_subcommand -a %q", topList))
		for _, sub := range root.Subcommands {
			lines = append(lines, fmt.Sprintf(
				"complete -c vat -n \"__fish_seen_subcommand_from %s\" -d %q", sub.Name, sub.Summary))
			for _, nestedCommand := range sub.Subcommands {
				lines = append(lines, fmt.Sprintf(
					"complete -c vat -n \"__fish_seen_subcommand_from %s\" -a %s -d %q",
					sub.Name, nestedCommand.Name, nestedCommand.Summary))
			}
		}
		return strings.Join(lines, "\n"), nil

	default:
		return "", usageErrorf("unsupported shell %q (want bash, zsh, or fish)", shell)
	}
}

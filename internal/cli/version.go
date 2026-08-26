package cli

import (
	"context"

	"github.com/takealook97/vat/internal/version"
)

func versionCommand() *Command {
	return &Command{
		Name:    "version",
		Summary: "Print the build identity",
		Usage:   "vat version [--short]",
		Run: func(ctx context.Context, env *Env, args []string) error {
			set := newFlagSet("version")
			short := set.Bool("short", false, "print only the semantic version")
			if err := parseFlags(set, args); err != nil {
				return err
			}
			if env.JSON {
				return emitJSON(env, map[string]string{
					"version": version.Short(),
					"commit":  version.Revision(),
					"date":    version.BuildDate(),
				})
			}
			if *short {
				env.Printer.Println(version.Short())
				return nil
			}
			env.Printer.Println(version.Long())
			return nil
		},
	}
}

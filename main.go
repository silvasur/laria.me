package main

import (
	"flag"
	"fmt"
	"os"

	"code.laria.me/laria.me/environment"
)

type subcmd func(progname string, env *environment.Env, args []string)

func main() {
	subcmds := map[string]subcmd{
		"serve":  cmdServe,
		"update": cmdUpdate,
	}

	progname := os.Args[0]

	flagSet := flag.NewFlagSet(progname, flag.ExitOnError)
	flagSet.Usage = func() {
		fmt.Fprintf(flagSet.Output(), "Usage: %s [options] <command> [command specific options]\n", progname)
		fmt.Fprintln(flagSet.Output(), "Where options can be any of:")
		flagSet.PrintDefaults()
		fmt.Fprintln(flagSet.Output(), "")
		fmt.Fprintln(flagSet.Output(), "And command is one of:")
		for n := range subcmds {
			fmt.Fprintf(flagSet.Output(), "  %s\n", n)
		}
	}

	configPath := flagSet.String("config", "", "An optional config path")
	flagSet.Parse(os.Args[1:])

	args := flagSet.Args()
	if len(args) == 0 {
		flagSet.Usage()
		os.Exit(1)
	}

	cmdName := args[0]
	args = args[1:]

	env := environment.New(*configPath)
	cmd, ok := subcmds[cmdName]
	if !ok {
		fmt.Fprintf(os.Stderr, "%s: Unknown command %s\nSee %s -help for valid commands\n", progname, cmdName, progname)
		os.Exit(1)
	}

	cmd(progname, env, args)
}
